"""Which base labs a variant can be generated from.

Backed by ``dreadgoad lab list --json`` rather than a second implementation of
the same directory walk in Python. Discovery already knows things the console
would otherwise have to re-derive and keep in step — which providers each lab
ships terraform for, and which hosts it defines — and getting that from the CLI
means it cannot drift from what ``variant generate`` will actually accept.

The one thing added on top is whether a lab is itself a *generated* variant.
``lab.DiscoverLabs`` filters those by a substring match on the directory name
(``strings.Contains(name, "-variant-")``, discovery.go:44), which only catches
the old ``ad/GOAD-variant-1`` default. Every variant the console creates is
named ``<source>-<variant name>``, so none of them match, and they accumulate in
the list looking like base labs. The generator writes ``mapping.json`` into every
target it produces (generator.go:1197), so that file is the reliable marker, and
it is what this module reports.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
import typing as t

import yaml

from . import commands, paths, projectroot
from .cli import Capture, capture

# Written by the variant generator into every directory it produces. Presence is
# what distinguishes a generated variant from a lab someone authored.
_VARIANT_MARKER = "mapping.json"
_RANGE_MANIFEST = "range.yml"
_ACTIVE_DIRECTORY_KIND = "active-directory"


def _variant_support(lab_path: str | Path) -> tuple[bool, str | None]:
    """Return whether the GOAD variant generator supports ``lab_path``.

    Older Active Directory labs may have no manifest, so absence remains
    supported for compatibility. Once a range declares a kind, only
    ``active-directory`` is accepted: the generator does not transform service
    range infrastructure, Ansible roles, application data, or credentials.
    """
    if not str(lab_path):
        return True, None
    manifest_path = Path(lab_path) / _RANGE_MANIFEST
    if not manifest_path.exists():
        return True, None
    try:
        raw = yaml.safe_load(manifest_path.read_text())
    except (OSError, yaml.YAMLError) as exc:
        return False, f"cannot read range manifest {manifest_path}: {exc}"
    kind = raw.get("kind") if isinstance(raw, dict) else None
    if kind == _ACTIVE_DIRECTORY_KIND:
        return True, None
    if not kind:
        return False, f"range manifest {manifest_path} has no kind"
    return (
        False,
        "variants are only supported for active-directory ranges; "
        f"{Path(lab_path).name} declares kind {kind!r}",
    )


def require_variant_source_supported(project_root: str | Path, source: str) -> None:
    """Raise before console session creation mutates files for a bad source."""
    source_path = Path(source or "ad/GOAD").expanduser()
    if not source_path.is_absolute():
        source_path = Path(project_root) / source_path
    supported, reason = _variant_support(source_path)
    if not supported:
        raise ValueError(reason or f"variant source {source_path} is unsupported")


async def discover_labs(
    config_path: str | None = None,
    capture_command: Capture | None = None,
) -> list[dict[str, t.Any]]:
    """List the labs available as a ``variant_source``, newest CLI view.

    ``config_path`` scopes discovery to that config's own tree, the way every
    other spawn does (projectroot.run_cwd). It is optional because the modal
    needs this list *before* a config exists when creating one — and a config
    created by the console lives under the repo, so the repo root is the right
    project root for it either way.

    Returns [] rather than raising: a missing binary or an unreadable ``ad/``
    should leave the operator with a free-text fallback, not a modal that
    cannot open.
    """
    argv = [commands.resolve_bin(str(paths.repo_root()))]
    if config_path:
        argv += ["--config", str(config_path)]
        cwd = projectroot.run_cwd(
            {"anchor": {"config_path": config_path}}, paths.repo_root()
        )
    else:
        # No config yet. `lab list` falls back to its working directory as the
        # project root, so this must be a tree that actually has an `ad/`.
        cwd = str(paths.repo_root())
    argv += ["lab", "list", "--json"]

    runner = capture_command or capture
    try:
        return_code, stdout, _stderr = await runner(argv, cwd)
    except (OSError, ValueError):
        # A missing or non-executable binary raises from create_subprocess_exec
        # rather than returning a non-zero code, so the return-code check below
        # never sees it. This is the *most likely* failure here — resolve_bin
        # falls back to an expected path when nothing is built — and letting it
        # out turns "no labs to list" into a 500 on the whole modal.
        return []
    if return_code != 0:
        return []
    try:
        found = json.loads(stdout)
    except Exception:  # noqa: BLE001
        return []
    if not isinstance(found, list):
        return []

    labs: list[dict[str, t.Any]] = []
    for entry in found:
        if not isinstance(entry, dict) or not entry.get("name"):
            continue
        name = str(entry["name"])
        path = str(entry.get("path") or "")
        variant_supported, _ = _variant_support(path)
        cli_variant_supported = entry.get("variant_supported")
        if isinstance(cli_variant_supported, bool):
            variant_supported = cli_variant_supported
        labs.append(
            {
                "name": name,
                "display_name": str(entry.get("display_name") or name),
                # What goes into `variant_source`, which is repo-relative while
                # the CLI reports an absolute path. Labs always live at
                # <project root>/ad/<name> (discovery.go:32,48), so this is a
                # reconstruction rather than a guess.
                "dir": f"ad/{name}",
                "providers": entry.get("providers") or [],
                "provider_settings": entry.get("provider_settings") or {},
                "hosts": entry.get("hosts") or [],
                "kind": str(entry.get("kind") or "active-directory"),
                "variant_supported": variant_supported,
                "generated": bool(path)
                and os.path.isfile(os.path.join(path, _VARIANT_MARKER)),
            }
        )
    # Base labs first, then generated variants; alphabetical within each group.
    labs.sort(key=lambda lab: (lab["generated"], lab["name"].lower()))
    return labs
