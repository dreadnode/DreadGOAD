"""Post-command ingestion hook (design §6.2, §6.4).

After a command, discover live instances (via ``lab status --json``) and
overlay their cloud state onto the range's config-seeded hosts. Overlay-only:
config hosts with no live instance go ``absent``; live instances with no
config host are ignored. The mapping is pure and unit-tested; ``run_check``
wires it to the CLI + DB.
"""

from __future__ import annotations

import json
import typing as t
from datetime import datetime, timezone

from . import commands, labconfig, paths
from .cli import capture

# Cloud power state → our host.status enum (§6.3).
_STATE = {
    "running": "running",
    "stopped": "stopped",
    "deallocated": "stopped",
    "pending": "provisioning",
    "starting": "provisioning",
    "creating": "provisioning",
    "terminated": "absent",
}

# Infra nodes correlate via aliases (the cloud instance name differs from the
# node id, e.g. the attack box VM is named "…kali…").
_ALIASES = {
    "attackbox": ["attackbox", "kali", "attack"],
    "bastion": ["bastion"],
}


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _norm_state(state: str | None) -> str:
    return _STATE.get((state or "").lower(), "unknown")


def _match(
    host: dict[str, t.Any], instances: list[dict[str, t.Any]]
) -> dict[str, t.Any] | None:
    """Find the instance whose name contains the host id (or an alias)."""
    hid = str(host["id"]).lower()
    aliases = _ALIASES.get(hid, [hid])
    for inst in instances:
        name = str(inst.get("name") or "").lower()
        if any(a in name for a in aliases):
            return inst
    return None


def map_range_status(
    rng: dict[str, t.Any], instances: list[dict[str, t.Any]], now: str | None = None
) -> dict[str, t.Any]:
    """Overlay live instance state onto range hosts (pure; §6.4).

    Returns a new range doc. Matched hosts get status/ip/cloud_id refreshed;
    unmatched config hosts go ``absent``. Unmatched instances are ignored.
    """
    now = now or _now()
    hosts_out: list[dict[str, t.Any]] = []
    for host in rng.get("hosts", []):
        h = dict(host)
        inst = _match(host, instances)
        if inst is None:
            h["status"] = "absent"
        else:
            h["status"] = _norm_state(inst.get("state"))
            h["ip_private"] = inst.get("private_ip") or h.get("ip_private")
            h["cloud_id"] = inst.get("id") or h.get("cloud_id")
        h["last_checked_at"] = now
        hosts_out.append(h)
    out = dict(rng)
    out["hosts"] = hosts_out
    out["last_checked_at"] = now
    return out


def summarize_changes(
    before: dict[str, t.Any], after: dict[str, t.Any]
) -> dict[str, t.Any]:
    """Build a check_run payload: which hosts changed status."""
    prev = {h["id"]: h.get("status") for h in before.get("hosts", [])}
    changes = []
    for h in after.get("hosts", []):
        old = prev.get(h["id"])
        if old != h.get("status"):
            changes.append({"id": h["id"], "from": old, "to": h.get("status")})
    return {"hosts_updated": len(changes), "changes": changes}


async def run_check(app: t.Any, session_id: str) -> dict[str, t.Any]:
    """Discover live state and overlay it onto the range (§6.4 flow).

    On failure: leave the range untouched (stale), don't advance
    ``last_checked_at``, mark the session ``error``, and return an error
    check_run payload.
    """
    db = app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        return {"error": "session/range not found"}

    argv = commands.build_argv(session, "/instances", repo_root=str(paths.repo_root()))
    try:
        # Separate streams: stderr noise must not corrupt the JSON on stdout.
        rc, out, err = await capture(argv, cwd=str(paths.repo_root()))
        if rc != 0:
            raise RuntimeError(f"lab status --json exited {rc}: {err[-500:]}")
        instances = json.loads(out)
    except Exception as exc:  # noqa: BLE001
        await app.state.sessions.set_status(session_id, "error")
        return {"error": str(exc)}

    updated = map_range_status(rng, instances)
    payload = summarize_changes(rng, updated)
    await db.upsert_range(session_id, updated)
    return payload


async def apply_health(app: t.Any, session_id: str, healthy: bool) -> None:
    """Set the health overlay on config hosts after /health (§6.4 two write paths).

    v1 applies a range-level verdict from the command's exit code (per-host
    health needs a --json health-check; noted as a follow-up).
    """
    db = app.state.db
    rng = await db.get_range(session_id)
    if rng is None:
        return
    verdict = "healthy" if healthy else "unhealthy"
    for h in rng.get("hosts", []):
        if h.get("source") == "config":
            h["health"] = verdict
    await db.upsert_range(session_id, rng)


async def reseed(app: t.Any, session_id: str) -> None:
    """Re-seed topology after /extensions or /variant change the node set (§6.3)."""
    db = app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        return
    snap = session.get("snapshot", {})
    cfg = labconfig.lab_config_path(str(paths.repo_root()), snap.get("lab"))
    seeded = labconfig.seed_topology(cfg, snap.get("provider"))
    await db.upsert_range(session_id, labconfig.merge_reseed(rng, seeded))
