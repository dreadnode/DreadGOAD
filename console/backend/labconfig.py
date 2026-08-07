"""Derive session snapshots and seed range topology from lab config (§4.3, §6.3).

Two inputs:
  - ``dreadgoad.yaml``      → the session *snapshot* (provider/region file-level;
                             variant/lab/network per-env)
  - ``ad/<lab>/data/config.json`` → the range *topology* (hosts + roles)

The snapshot is a cache derived from the ``(config_path, env)`` anchor; the
topology is the config-seeded node set the ingestion hook later overlays.
"""

from __future__ import annotations

import json
import os
import shutil
import typing as t

import yaml

# config.json host `type` → RangeView role (§6.3).
_ROLE_BY_TYPE = {
    "dc": "dc",
    "server": "member",
    "workstation": "workstation",
}


def list_environments(config_path: str) -> dict[str, t.Any]:
    """List the environment names defined in a ``dreadgoad.yaml`` (+ provider/region).

    Drives the new-session env dropdown. Raises FileNotFoundError / YAMLError /
    OSError on a bad path or malformed file (surfaced as 400 by the endpoint).
    """
    with open(config_path) as f:
        data = yaml.safe_load(f) or {}
    if not isinstance(data, dict):
        raise ValueError(
            f"{config_path} is not a valid config (expected a YAML mapping)"
        )
    return {
        "environments": list((data.get("environments") or {}).keys()),
        "provider": data.get("provider"),
        "region": data.get("region"),
    }


def derive_snapshot(config_path: str, env: str) -> dict[str, t.Any]:
    """Build a session ``snapshot`` from ``(config_path, env)``.

    Provider/region are file-level (top of ``dreadgoad.yaml``); variant/lab/
    network come from the named env. Credentials are never included.
    """
    with open(config_path) as f:
        data = yaml.safe_load(f) or {}

    provider = data.get("provider")
    region = data.get("region")
    envs = data.get("environments") or {}
    if env not in envs:
        available = ", ".join(sorted(envs)) or "none"
        raise ValueError(
            f"environment {env!r} not found in {config_path} (available: {available})"
        )
    e = envs[env] or {}

    variant_target = e.get("variant_target")
    variant_source = e.get("variant_source")
    lab = variant_target or variant_source

    snapshot: dict[str, t.Any] = {
        "provider": provider,
        "region": region,
        "lab": lab,
        "variant_name": e.get("variant_name"),
        "vpc_cidr": e.get("vpc_cidr"),
        "attack_box": None,  # discovered post-deploy
    }
    # Provider-specific block (selectors only, never secrets).
    if provider == "aws":
        snapshot["aws"] = {"profile": None}
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


def _blank_dynamic() -> dict[str, t.Any]:
    return {
        "status": "unknown",
        "health": "unknown",
        "ip_private": None,
        "ip_public": None,
        "cloud_id": None,
        "cloud_name": None,  # provider VM name, learned from the ingestion hook
        "last_checked_at": None,
    }


def seed_topology(
    lab_config_path: str | None, provider: str | None
) -> dict[str, t.Any]:
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

    hosts: list[dict[str, t.Any]] = []
    for _key, h in hosts_cfg.items():
        hostname = h.get("hostname", _key)
        host = {
            "id": hostname,
            # The config key (``dc01``) is the CLI's host *role*, and it's what
            # cloud instances are named after — a variant renames the hostname
            # (``solar``) but not the VM. Keep it for instance correlation and
            # for matching per-host health results (§6.4).
            "key": _key,
            "hostname": hostname,
            "role": _role_for(h.get("type", "")),
            "source": "config",
            "domain": h.get("domain"),
            **_blank_dynamic(),
        }
        hosts.append(host)

    # Infra nodes (not in the lab config).
    hosts.append(
        {
            "id": "attackbox",
            "key": "attackbox",
            "hostname": "attackbox",
            "role": "attackbox",
            "source": "infra",
            "domain": None,
            **_blank_dynamic(),
        }
    )
    if provider == "azure":
        hosts.append(
            {
                "id": "bastion",
                "key": "bastion",
                "hostname": "bastion",
                "role": "bastion",
                "source": "infra",
                "domain": None,
                **_blank_dynamic(),
            }
        )

    return {"hosts": hosts, "edges": [], "layout": {}, "last_checked_at": None}


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
    existing: dict[str, t.Any], seeded: dict[str, t.Any]
) -> dict[str, t.Any]:
    """Re-seed a range's node set while preserving live state + layout (§6.3).

    Used after ``/extensions`` / ``/variant`` change the topology: the node set
    becomes ``seeded`` (adds new machines like ELK, drops removed ones), but
    surviving hosts keep their dynamic fields (status/health/ip/…) and saved
    positions.
    """
    old = {h["id"]: h for h in existing.get("hosts", [])}
    hosts: list[dict[str, t.Any]] = []
    for h in seeded.get("hosts", []):
        if h["id"] in old:
            merged = dict(h)
            prev = old[h["id"]]
            for k in _DYNAMIC_FIELDS:
                if k in prev:
                    merged[k] = prev[k]
            hosts.append(merged)
        else:
            hosts.append(h)
    keep = {h["id"] for h in hosts}
    layout = {k: v for k, v in existing.get("layout", {}).items() if k in keep}
    out = dict(existing)
    out["hosts"] = hosts
    out["edges"] = seeded.get("edges", [])
    out["layout"] = layout
    return out


def lab_config_path(repo_root: str, lab: str | None) -> str | None:
    """Resolve ``ad/<lab>/data/config.json`` under the repo root.

    ``lab`` is a repo-relative dir like ``ad/GOAD-dreadindex``. Returns None if
    ``lab`` is unset.
    """
    if not lab:
        return None
    return os.path.join(repo_root, lab, "data", "config.json")


def write_new_env(
    config_path: str,
    env_name: str,
    env_fields: dict[str, t.Any],
    top_level: dict[str, t.Any] | None = None,
) -> str:
    """Add/replace an env entry in a ``dreadgoad.yaml`` (create-new flow, §4.3).

    Backs up the file first (if it exists). ``top_level`` sets file-level keys
    (``provider``/``region``) shared by all envs. Note: round-tripping via
    ``safe_dump`` does not preserve comments — the backup is the safety net.
    """
    data: dict[str, t.Any] = {}
    if os.path.isfile(config_path):
        with open(config_path) as f:
            data = yaml.safe_load(f) or {}
        backup_yaml(config_path)

    if top_level:
        data.update(top_level)
    envs = data.setdefault("environments", {})
    envs[env_name] = env_fields

    with open(config_path, "w") as f:
        yaml.safe_dump(data, f, sort_keys=False)
    return config_path


def backup_yaml(config_path: str) -> str:
    """Write a versioned backup copy of a yaml before mutating it (§4.3).

    Returns the backup path (``<config>.bak.N`` with the next free N).
    """
    n = 1
    while os.path.exists(f"{config_path}.bak.{n}"):
        n += 1
    backup = f"{config_path}.bak.{n}"
    shutil.copy2(config_path, backup)
    return backup
