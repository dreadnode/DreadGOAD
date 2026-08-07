"""Extension discovery and topology reseeding after mutating commands."""

from __future__ import annotations

import json
import typing as t

from . import commands, labconfig, paths
from .cli import capture

Capture = t.Callable[[list[str], str], t.Awaitable[tuple[int, str, str]]]
_EXT_ROLE = "linux"


async def extension_nodes(
    session: dict[str, t.Any], capture_command: Capture | None = None
) -> list[dict[str, t.Any]]:
    """Return nodes belonging to enabled extensions."""
    anchor = session.get("anchor", {})
    config_path, env = anchor.get("config_path"), anchor.get("env")
    if not config_path or not env:
        return []
    argv = [
        commands.resolve_bin(str(paths.repo_root())),
        "--config",
        str(config_path),
        "--env",
        str(env),
        "extension",
        "list",
        "--json",
    ]
    runner = capture_command or capture
    return_code, stdout, _stderr = await runner(argv, str(paths.repo_root()))
    if return_code != 0:
        return []
    try:
        extensions = json.loads(stdout)
    except Exception:  # noqa: BLE001
        return []
    nodes = []
    for extension in extensions:
        if not extension.get("enabled"):
            continue
        for machine in extension.get("machines") or []:
            nodes.append(
                {
                    "id": machine,
                    "hostname": machine,
                    "role": _EXT_ROLE,
                    "source": "extension",
                    "domain": None,
                    "status": "unknown",
                    "health": "unknown",
                    "ip_private": None,
                    "ip_public": None,
                    "cloud_id": None,
                    "last_checked_at": None,
                }
            )
    return nodes


async def reseed(
    app: t.Any,
    session_id: str,
    capture_command: Capture | None = None,
) -> None:
    """Re-seed topology while preserving live state/layout for surviving nodes."""
    db = app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        return
    snapshot = session.get("snapshot", {})
    config = labconfig.lab_config_path(str(paths.repo_root()), snapshot.get("lab"))
    seeded = labconfig.seed_topology(config, snapshot.get("provider"))
    existing_ids = {host["id"] for host in seeded["hosts"]}
    for node in await extension_nodes(session, capture_command):
        if node["id"] not in existing_ids:
            seeded["hosts"].append(node)
            existing_ids.add(node["id"])
    await db.upsert_range(session_id, labconfig.merge_reseed(rng, seeded))
