"""Which config files the console knows about, and where new ones go.

Split from :mod:`labconfig`, which reads and writes what is *inside* a config.
This module answers the questions that come first: which ``dreadgoad.yaml``
files exist, where a new one should be written, and whether the provider it
names has credentials available to it.

The console has always been able to *drive* more than one config — every CLI
spawn carries ``--config`` (commands.py:413-415) and derives its working
directory from that config's own tree (projectroot.run_cwd). What was missing
was any way to see or create them, which is what this provides.
"""

from __future__ import annotations

import os
import re
import typing as t
from pathlib import Path

import yaml

from . import labconfig, paths

# Only these are offered in the create UI; see labconfig.CONSOLE_PROVIDERS for
# why the other two CLI providers are excluded.
PROVIDERS = labconfig.CONSOLE_PROVIDERS

_SLUG_STRIP = re.compile(r"[^a-z0-9]+")

# Suggestions for the region field — NOT a closed set. The field stays free
# text because "which regions work" is not the same question for the two
# providers:
#
#   azure  Hosts come from a marketplace image (publisher/offer/sku in the
#          host terragrunt), which exists in essentially every region, so any
#          value here is plausible.
#   aws    Hosts resolve a warpgate-built AMI with owners = ["self"]
#          (infra/goad-deployment/.../goad/dc01/terragrunt.hcl). AMIs are
#          region-scoped and are not copied automatically, so a region only
#          works where one has been built — offering all ~35 as a closed
#          dropdown would present mostly choices that fail at apply time.
#
# The regions this repo already deploys into are listed first, since those are
# the ones known to work here.
COMMON_REGIONS: dict[str, tuple[str, ...]] = {
    "aws": (
        "us-west-1",
        "us-east-2",
        "us-east-1",
        "us-west-2",
        "eu-west-1",
        "eu-west-2",
        "eu-central-1",
        "ap-southeast-2",
    ),
    "azure": (
        "centralus",
        "eastus",
        "eastus2",
        "westus2",
        "westus3",
        "northeurope",
        "westeurope",
        "uksouth",
        "australiaeast",
    ),
}


def slug_for(name: str) -> str:
    """Reduce a user-supplied config name to a safe bare filename stem.

    The result is used to build a path, so it is restricted to ``[a-z0-9-]``
    rather than merely escaped: anything that could traverse (``/``, ``..``),
    hide (a leading dot), or collide with shell/YAML handling is dropped rather
    than encoded. Raises ValueError when nothing usable survives, because a
    silent fallback name would put the config somewhere the operator did not
    ask for and would not think to look.
    """
    slug = _SLUG_STRIP.sub("-", name.strip().lower()).strip("-")[:48]
    if not slug:
        raise ValueError(
            f"config name {name!r} has no letters or digits in it — "
            "use something like 'azure-lab'"
        )
    return slug


def path_for(name: str) -> Path:
    """Absolute path a new config called ``name`` will be written to.

    The containment assertion is redundant against :func:`slug_for`'s character
    set and is kept deliberately: it is the check that still holds if that
    pattern is ever loosened, and this value comes from the browser.
    """
    root = paths.configs_root().resolve()
    candidate = (root / f"{slug_for(name)}.yaml").resolve()
    if candidate.parent != root:
        raise ValueError(f"refusing to write a config outside {root}")
    return candidate


def default_config_path() -> str:
    """The repo-root ``dreadgoad.yaml`` the console starts out pointed at."""
    return str(paths.repo_root() / "dreadgoad.yaml")


def _summarise(path: str, source: str) -> dict[str, t.Any]:
    """Describe one config for the picker, reporting rather than raising.

    A config that has gone missing or unparseable still has to appear in the
    list: it is very likely the one the operator is looking for, and dropping
    it silently turns "my config is broken" into "my config vanished".
    """
    entry: dict[str, t.Any] = {
        "path": path,
        "name": os.path.basename(path),
        "source": source,
        "provider": None,
        "region": None,
        "environments": [],
        "error": None,
    }
    try:
        info = labconfig.list_environments(path)
    except FileNotFoundError:
        entry["error"] = "file no longer exists"
    except yaml.YAMLError as exc:
        entry["error"] = f"not valid YAML: {exc}"
    except (ValueError, OSError) as exc:
        entry["error"] = str(exc)
    else:
        entry["provider"] = info.get("provider")
        entry["region"] = info.get("region")
        entry["environments"] = info.get("environments") or []
    return entry


# Ordering for the picker: the config the console defaults to, then the ones it
# created, then ones learned from existing sessions. Within a group, by path.
_SOURCE_ORDER = {"default": 0, "managed": 1, "session": 2}


def known_configs(anchor_paths: t.Iterable[str] = ()) -> list[dict[str, t.Any]]:
    """Every config the console can offer, deduplicated and summarised.

    Three sources, in precedence order: the repo-root default, files under
    :func:`paths.configs_root`, and the ``config_path`` anchors of existing
    sessions. The last is what makes a config the operator typed by hand — one
    in another checkout, say — stay in the list afterwards, without needing a
    registry table to keep in sync with the sessions that are the real record.

    Deduplicated on the resolved path so the same file reached two ways appears
    once, keeping the source it was first seen under.

    The resolved form is what gets reported, so the surviving entry has one
    canonical path rather than whichever spelling happened to be seen first.
    The same file genuinely arrives spelled differently: on macOS the configs
    dir is reached as ``/var/...`` by the glob and ``/private/var/...`` once
    resolved, and session anchors are stored exactly as the operator typed them
    (sessions.py:67 keeps ``config_path`` verbatim). Without canonicalising,
    which of those two the picker displayed would depend on iteration order.
    """
    found: dict[str, dict[str, t.Any]] = {}

    def add(path: str, source: str) -> None:
        try:
            key = str(Path(path).expanduser().resolve())
        except OSError:
            key = path
        if key not in found:
            found[key] = _summarise(key, source)

    add(default_config_path(), "default")
    root = paths.configs_root()
    for entry in sorted(root.iterdir()) if root.is_dir() else []:
        if entry.is_file() and entry.suffix in (".yaml", ".yml"):
            add(str(entry), "managed")
    for path in anchor_paths:
        if path:
            add(str(path), "session")

    return sorted(
        found.values(),
        key=lambda c: (_SOURCE_ORDER.get(c["source"], 9), c["path"]),
    )


# Where each provider's credentials come from when nothing is set explicitly.
# Checked as environment variables and well-known files only — never by running
# `aws sts get-caller-identity` or `az account show`. Those are subprocesses in
# a request handler that may be absent, may prompt, and can block on a stale
# token; the answer is not worth stalling the modal for.
_CREDENTIAL_SOURCES: dict[str, tuple[tuple[str, ...], tuple[str, ...], str]] = {
    "aws": (
        (
            "AWS_PROFILE",
            "AWS_ACCESS_KEY_ID",
            "AWS_ROLE_ARN",
            "AWS_WEB_IDENTITY_TOKEN_FILE",
            "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
        ),
        ("~/.aws/credentials", "~/.aws/config"),
        "AWS_PROFILE or ~/.aws",
    ),
    "azure": (
        ("AZURE_CLIENT_ID", "AZURE_TENANT_ID"),
        ("~/.azure/azureProfile.json",),
        "az login or AZURE_CLIENT_ID",
    ),
}


def credential_hint(provider: str) -> str | None:
    """An advisory note when a provider's credentials aren't visible, else None.

    Hedged on purpose, and never blocking. The checks below cannot see an EC2
    instance role, a credential_process, or an SSO session cached under a name
    this does not know, so a false "not found" is entirely possible — and a
    warning that is sometimes wrong is only useful if it says so. The reverse
    error is cheap: finding a file proves nothing about whether the credentials
    in it are valid, which is what ``dreadgoad doctor`` is for.
    """
    sources = _CREDENTIAL_SOURCES.get(provider)
    if sources is None:
        return None
    env_names, files, where = sources
    if any(os.environ.get(name) for name in env_names):
        return None
    if any(os.path.exists(os.path.expanduser(f)) for f in files):
        return None
    return (
        f"No {provider} credentials found in the usual places ({where}). "
        f"Creating this is still fine — deploying will fail until they exist. "
        f"Run `dreadgoad doctor` to check properly."
    )
