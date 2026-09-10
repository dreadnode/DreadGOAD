"""Derive session snapshots and seed range topology from lab config (§4.3, §6.3).

Two inputs:
  - ``dreadgoad.yaml``      → the session *snapshot* (provider/region/lab
                             resolved per environment; variant/network per-env)
  - ``ad/<lab>/data/config.json`` → the range *topology* (hosts + roles)

The snapshot is a cache derived from the ``(config_path, env)`` anchor; the
topology is the config-seeded node set the ingestion hook later overlays.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import typing as t

import ruamel.yaml
import yaml

from . import projectroot
from .schemas import (
    RangeDocument,
    RangeHost,
    SessionDocument,
    SessionSnapshot,
    new_range_host,
)

# config.json host `type` → RangeView role (§6.3).
_ROLE_BY_TYPE = {
    "dc": "dc",
    "server": "member",
    "workstation": "workstation",
}

# Every provider the Go CLI knows (cli/internal/provider/factory.go:10-13).
CLI_PROVIDERS = ("aws", "azure", "proxmox", "ludus")

# The subset the console can actually drive end to end. `derive_snapshot` only
# builds a provider block for these two, `seed_topology` only knows azure's
# bastion, and the frontend's CONNECT planner dead-ends on anything else
# (frontend/src/connect.ts:90). A session on proxmox or ludus would be created
# happily and then be unable to render or connect, so the create UI offers only
# these while the CLI keeps supporting the rest.
CONSOLE_PROVIDERS = ("aws", "azure")

# Matches cli/internal/config.validLabName. Explicit lab names are joined below
# ``ad/``, so separators and dot segments must never be accepted here.
_VALID_LAB_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_-]*$")


def _environments_of(data: dict[str, t.Any], config_path: str) -> dict[str, t.Any]:
    """The ``environments`` mapping, or ValueError naming what was found instead.

    ``environments:`` written as a YAML *list* is the easy mistake — it is how
    almost every other list-shaped key in a config file looks. Without this the
    shape error surfaced as an AttributeError from ``.keys()``, which no caller
    catches: the environments endpoint lists ValueError/YAMLError/OSError and
    would have returned a 500 for what is a typo in the operator's own file.
    """
    envs = data.get("environments")
    if envs is None:
        # Absent, or present-but-empty (``environments:`` with nothing under
        # it). Returning a fresh mapping is only safe for readers; writers must
        # attach it to the document themselves — see write_new_env.
        return {}
    if not isinstance(envs, dict):
        raise ValueError(
            f"{config_path}: 'environments' must be a mapping of name to "
            f"settings, but it is a {type(envs).__name__}. It should read "
            f"'environments:' followed by indented 'name:' entries, not a "
            f"'-' list."
        )
    # NOT ``or {}``: an existing empty mapping is falsy, and substituting a new
    # one for it would hand a writer a detached dict whose contents never reach
    # the file.
    return envs


def list_environments(config_path: str) -> dict[str, t.Any]:
    """List the environment names defined in a ``dreadgoad.yaml`` (+ provider/region).

    Drives the new-session env dropdown. Raises FileNotFoundError / YAMLError /
    ValueError / OSError on a bad path or malformed file (surfaced as 400 by the
    endpoint).
    """
    with open(config_path) as f:
        data = yaml.safe_load(f) or {}
    if not isinstance(data, dict):
        raise ValueError(
            f"{config_path} is not a valid config (expected a YAML mapping)"
        )
    envs = _environments_of(data, config_path)
    file_provider = data.get("provider")
    file_region = data.get("region")

    # Report the values the CLI will actually resolve for each environment.
    # Keeping the file-level values preserves the existing API contract for
    # create-new-environment, while the maps let an attached environment select
    # its own provider and region without inheriting another environment's
    # cloud context in the UI.
    return {
        "environments": list(envs.keys()),
        "provider": file_provider,
        "region": file_region,
        "env_providers": {
            name: resolve_provider(settings, file_provider)
            for name, settings in envs.items()
        },
        "env_regions": {
            name: resolve_region(settings, file_region)
            for name, settings in envs.items()
        },
    }


def resolve_provider(env_settings: t.Any, file_provider: t.Any) -> str:
    """The provider the CLI will actually use for one environment.

    Mirrors ``Config.ResolvedProvider``: an environment override wins, then the
    file-level setting, and AWS remains the CLI's compatibility default when
    neither is set. Invalid environment shapes are handled like an absent
    override here; ``_environments_of`` owns validation of the outer mapping.
    """
    if isinstance(env_settings, dict) and env_settings.get("provider"):
        return str(env_settings["provider"])
    return str(file_provider or "aws")


def resolve_region(env_settings: t.Any, file_region: t.Any) -> t.Any:
    """The region the CLI will actually use for one environment.

    Shared by :func:`list_environments` and :func:`derive_snapshot` so the
    warning and session header cannot disagree. Mirrors ``Config.ResolvedRegion``:
    the environment value wins and the file-level value is the fallback for
    every cloud provider.
    """
    env_region = env_settings.get("region") if isinstance(env_settings, dict) else None
    return env_region or file_region


def resolve_lab(env_settings: t.Any, file_lab: t.Any) -> str:
    """Resolve a console lab path using CLI-compatible explicit-lab rules.

    Existing variant configurations store repo-relative paths and retain their
    established target/source precedence. Otherwise this mirrors
    ``Config.ResolvedLab``: the environment's bare lab name wins, followed by
    the file-level name and the backward-compatible ``GOAD`` default. Explicit
    names are validated before being placed below ``ad/``.
    """
    settings = env_settings if isinstance(env_settings, dict) else {}
    variant_target = settings.get("variant_target")
    variant_source = settings.get("variant_source")
    if variant_target:
        return str(variant_target)
    if variant_source:
        return str(variant_source)

    lab = settings.get("lab") or file_lab or "GOAD"
    if not isinstance(lab, str) or not _VALID_LAB_NAME.fullmatch(lab):
        raise ValueError(
            f"invalid lab name {lab!r}: use letters, numbers, underscores, and hyphens"
        )
    return os.path.join("ad", lab)


def derive_snapshot(config_path: str, env: str) -> SessionSnapshot:
    """Build a session ``snapshot`` from ``(config_path, env)``.

    Provider/region/lab follow the CLI's per-environment resolution rules;
    variant/network come from the named env. Credentials are never included.
    """
    with open(config_path) as f:
        data = yaml.safe_load(f) or {}
    if not isinstance(data, dict):
        raise ValueError(
            f"{config_path} is not a valid config (expected a YAML mapping)"
        )

    envs = _environments_of(data, config_path)
    if env not in envs:
        available = ", ".join(sorted(envs)) or "none"
        raise ValueError(
            f"environment {env!r} not found in {config_path} (available: {available})"
        )
    e = envs[env] or {}

    provider = resolve_provider(e, data.get("provider"))

    # Mirrors Config.ResolvedRegion: the environment's own region wins, and the
    # file-level key is the fallback for environments that
    # don't declare one. Reading only the file-level key made the header show a
    # region the CLI would not actually use. The CLI's highest-precedence source
    # — --region / DREADGOAD_REGION — is deliberately not consulted: it belongs
    # to a single invocation, not to the environment this snapshot describes.
    region = resolve_region(e, data.get("region"))

    lab = resolve_lab(e, data.get("lab"))

    snapshot: SessionSnapshot = {
        "provider": provider,
        "region": region,
        "lab": lab,
        "variant_name": e.get("variant_name"),
        "vpc_cidr": e.get("vpc_cidr"),
        "attack_box": None,  # discovered post-deploy
    }
    # Provider-specific block (selectors only, never secrets).
    if provider == "aws":
        snapshot["aws"] = {"profile": os.environ.get("AWS_PROFILE") or None}
    elif provider == "azure":
        # Where the range landed (subscription/resource group) is NOT here —
        # the ingestion hook learns it post-deploy and writes it to the
        # snapshot's provider-neutral ``account``/``group`` (see
        # inventory_sync.py), so the RangeView header reads one pair of keys
        # for every provider.
        snapshot["azure"] = {"ssh_key": None, "ssh_user": "kali"}
    return snapshot


def _role_for(host_type: str) -> str:
    return _ROLE_BY_TYPE.get((host_type or "").lower(), "other")


def seed_topology(lab_config_path: str | None, provider: str | None) -> RangeDocument:
    """Seed a range's node set from ``config.json`` + infra nodes (§6.3).

    3-way merge, v1 subset:
      - **config** hosts from ``config.json`` (``type`` → role)
      - **infra** nodes not in the lab config: attack box always; bastion for
        Azure (SSM has no bastion node on AWS)
    Extension machines are NOT produced here — ``topology_sync.reseed`` augments this
    with enabled extensions' machines (from ``extension list --json``) when
    ``/extensions`` runs.
    Edges are deferred (v1 nodes-only), so ``edges`` is empty.

    If ``lab_config_path`` is None or missing (greenfield range whose variant
    isn't generated yet), only infra nodes are seeded; a later re-seed picks up
    the config hosts once they exist.
    """
    hosts_cfg: dict[str, t.Any] = {}
    if lab_config_path and os.path.isfile(lab_config_path):
        with open(lab_config_path) as f:
            cfg = json.load(f)
        hosts_cfg = (cfg.get("lab") or {}).get("hosts") or {}

    hosts: list[RangeHost] = []
    for _key, h in hosts_cfg.items():
        hostname = h.get("hostname", _key)
        configured_os = h.get("os")
        operating_system = (
            configured_os.strip()
            if isinstance(configured_os, str) and configured_os.strip()
            else None
        )
        host = new_range_host(
            hostname,
            # The config key (``dc01``) is the CLI's host *role*, and it's what
            # cloud instances are named after — a variant renames the hostname
            # (``solar``) but not the VM. Keep it for instance correlation and
            # for matching per-host health results (§6.4).
            _key,
            hostname,
            _role_for(h.get("type", "")),
            "config",
            h.get("domain"),
            operating_system,
        )
        hosts.append(host)

    # Infra nodes (not in the lab config).
    hosts.append(
        new_range_host(
            "attackbox", "attackbox", "attackbox", "attackbox", "infra", None
        )
    )
    if provider == "azure":
        hosts.append(
            new_range_host("bastion", "bastion", "bastion", "bastion", "infra", None)
        )

    return {
        "hosts": hosts,
        "edges": [],
        "layout": {},
        "last_checked_at": None,
    }


_DYNAMIC_FIELDS = (
    "status",
    "health",
    "ip_private",
    "ip_public",
    "cloud_id",
    "cloud_name",
    "last_checked_at",
)


def merge_reseed(
    existing: t.Mapping[str, t.Any], seeded: t.Mapping[str, t.Any]
) -> RangeDocument:
    """Re-seed a range's node set while preserving live state + layout (§6.3).

    Used after ``/extensions`` / ``/variant`` change the topology: the node set
    becomes ``seeded`` (adds new machines like ELK, drops removed ones), but
    surviving hosts keep their dynamic fields (status/health/ip/…) and saved
    positions.
    """
    old = {h["id"]: h for h in existing.get("hosts", [])}
    hosts: list[RangeHost] = []
    for h in seeded.get("hosts", []):
        if h["id"] in old:
            merged: dict[str, t.Any] = dict(h)
            prev = old[h["id"]]
            for k in _DYNAMIC_FIELDS:
                if k in prev:
                    merged[k] = prev[k]
            hosts.append(t.cast(RangeHost, merged))
        else:
            hosts.append(h)
    keep = {h["id"] for h in hosts}
    layout = {k: v for k, v in existing.get("layout", {}).items() if k in keep}
    out = t.cast(RangeDocument, dict(existing))
    out["hosts"] = hosts
    out["edges"] = seeded.get("edges", [])
    out["layout"] = layout
    return out


def session_lab_config_path(session: SessionDocument, fallback_root: str) -> str | None:
    """Where a session's lab config lives, resolved in the config's own tree.

    ``lab`` is repo-relative (``ad/GOAD-redteam``), so it only means anything
    against the right root. Both seeders previously resolved it against the
    *console's* repo — which is correct only while every config lives there.
    A config in another checkout has its ``ad/`` in that checkout, so the lookup
    missed, ``seed_topology`` took its greenfield path, and the range came up
    with infra nodes and no hosts. Identical symptom to seeding before the
    variant exists, and reached by a completely different route.

    Mirrors what the CLI itself will do for this session (projectroot.run_cwd).
    """
    anchor = session.get("anchor") or {}
    config_path = anchor.get("config_path")
    env = anchor.get("env")
    root = (
        str(projectroot.resolve_root(config_path)[0]) if config_path else fallback_root
    )
    return lab_config_path(
        root,
        (session.get("snapshot") or {}).get("lab"),
        str(env) if env else None,
    )


def lab_config_path(
    repo_root: str, lab: str | None, env: str | None = None
) -> str | None:
    """Resolve an environment's lab config JSON under the repo root.

    ``lab`` is a repo-relative dir like ``ad/GOAD-dreadindex``. Returns None if
    ``lab`` is unset. Mirrors the CLI's legacy environment-config precedence:
    ``<env>-config.json`` wins when present, with ``config.json`` as fallback.
    """
    if not lab:
        return None
    data_dir = os.path.realpath(os.path.join(repo_root, lab, "data"))
    if env:
        environment_config = os.path.realpath(
            os.path.join(data_dir, f"{env}-config.json")
        )
        if os.path.commonpath((data_dir, environment_config)) != data_dir:
            raise ValueError(
                f"invalid environment name {env!r}: path separators forbidden"
            )
        if os.path.isfile(environment_config):
            return environment_config
    return os.path.join(data_dir, "config.json")


def _round_trip_yaml() -> ruamel.yaml.YAML:
    """A YAML handler that reads and writes a file without rewriting it.

    ruamel's round-trip mode keeps comments, key order, blank lines and quoting
    styles attached to the data, so dumping a loaded document reproduces
    everything it did not change.

    ``width`` is raised because the default (80) re-wraps long scalars that were
    on one line — a wrapped CIDR or resource path is still valid YAML but shows
    up as noise in the operator's diff of their own file.
    """
    handler = ruamel.yaml.YAML()
    handler.preserve_quotes = True
    handler.width = 4096
    return handler


def write_new_env(
    config_path: str,
    env_name: str,
    env_fields: dict[str, t.Any],
    top_level: dict[str, t.Any] | None = None,
) -> str:
    """Add/replace an env entry in a ``dreadgoad.yaml`` (create-new flow, §4.3).

    Backs up the file first (if it exists). ``top_level`` sets file-level keys
    (``provider``/``region``) shared by all envs.

    Rewritten through ruamel's round-trip loader rather than ``yaml.safe_dump``.
    The checked-in ``dreadgoad.yaml`` is a documented template — the Proxmox key
    reference, the provider and region hints — and safe_dump reproduced only the
    data, so adding one environment through the console deleted 44 lines of
    comments from a tracked file. The backup was the only thing standing between
    that and a silent loss, and a backup you have to notice is not a safety net.

    Falls back to a plain construction when the file does not exist yet: there
    is nothing to preserve, and a config created here gets its own file rather
    than sharing this one (see :func:`create_config`).
    """
    handler = _round_trip_yaml()
    data: t.Any = {}
    if os.path.isfile(config_path):
        try:
            with open(config_path) as f:
                data = handler.load(f)
        except ruamel.yaml.YAMLError as exc:
            # ruamel's YAMLError shares no ancestry with pyyaml's beyond
            # Exception, so it slips past every caller's `except yaml.YAMLError`
            # — the create-environment route turned a malformed config from a
            # 400 into a 500 the moment this function stopped using safe_load.
            # Converting here keeps which YAML library is in use an implementation
            # detail of this module, which is where that choice was made.
            raise ValueError(f"{config_path} is not valid YAML: {exc}") from exc
        if data is None:
            data = {}
        if not isinstance(data, dict):
            raise ValueError(
                f"{config_path} is not a valid config (expected a YAML mapping)"
            )
        backup_yaml(config_path)

    if top_level:
        data.update(top_level)
    # Attach the mapping to the document *before* resolving it, so `envs` is
    # always the object that gets dumped rather than a copy of it.
    if data.get("environments") is None:
        data["environments"] = {}
    envs = _environments_of(data, config_path)
    envs[env_name] = env_fields

    with open(config_path, "w") as f:
        handler.dump(data, f)
    return config_path


def create_config(
    config_path: str,
    provider: str,
    env_name: str,
    env_fields: dict[str, t.Any],
    region: str | None = None,
) -> str:
    """Write a brand-new ``dreadgoad.yaml`` with one environment in it.

    Deliberately NOT :func:`write_new_env` with an absent file. That function
    merges into whatever is already on disk and keeps a ``.bak`` — right for
    adding an environment, wrong here: pointed at an existing config it would
    silently adopt someone else's provider and environments as the "new" one.
    Refusing instead matches ``dreadgoad init`` (cli/cmd/init.go:51-53), which
    is the CLI-side equivalent of this call.

    ``region`` is written file-level rather than onto the environment because
    it is collected as a property of the config. ``Config.ResolveRegion``
    (config.go:474-485) treats the file-level key as the fallback for
    environments that don't declare their own, so a later per-environment
    region overrides this without the two disagreeing.

    Raises FileExistsError if ``config_path`` is taken, ValueError on an unknown
    provider or an empty environment name.
    """
    if provider not in CLI_PROVIDERS:
        raise ValueError(
            f"unknown provider {provider!r} (expected one of "
            f"{', '.join(CLI_PROVIDERS)})"
        )
    if not env_name.strip():
        raise ValueError("environment name is required")
    if os.path.exists(config_path):
        raise FileExistsError(
            f"{config_path} already exists — refusing to overwrite it"
        )

    data: dict[str, t.Any] = {"provider": provider}
    if region:
        data["region"] = region
    # The default environment for a bare `dreadgoad ...` run next to this file.
    # The console always passes --env explicitly (commands.py:413-415), so this
    # only matters when the operator picks the config up by hand — which is
    # exactly when a missing default is most confusing.
    data["env"] = env_name
    data["environments"] = {env_name: env_fields}

    os.makedirs(os.path.dirname(config_path) or ".", exist_ok=True)
    # Written 0o600, not the 0o644 of `dreadgoad config init` (config_cmd.go:112):
    # a proxmox password or ludus api_key can be added to this file later, and
    # widening permissions after the fact is a step nobody takes.
    fd = os.open(config_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        yaml.safe_dump(data, f, sort_keys=False)
    return config_path


MAX_BACKUPS = 5


def backup_yaml(config_path: str) -> str:
    """Write a versioned backup copy of a yaml before mutating it (§4.3).

    Returns the backup path (``<config>.bak.N`` with the next free N).
    Keeps at most :data:`MAX_BACKUPS` copies; older ones are removed.
    """
    parent = os.path.dirname(config_path) or "."
    prefix = os.path.basename(config_path) + ".bak."
    max_n = 0
    for entry in os.listdir(parent):
        if entry.startswith(prefix) and entry[len(prefix) :].isdigit():
            max_n = max(max_n, int(entry[len(prefix) :]))
    n = max_n + 1
    backup = f"{config_path}.bak.{n}"
    shutil.copy2(config_path, backup)
    for old in range(1, n + 1 - MAX_BACKUPS):
        try:
            os.remove(f"{config_path}.bak.{old}")
        except FileNotFoundError:
            pass
    return backup
