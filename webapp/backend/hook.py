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


def find_attack_box(instances: list[dict[str, t.Any]]) -> str | None:
    """The attack box's cloud id among live instances, or None (§5.2).

    ``/score`` needs the attack box to fetch the report, but it's only known
    post-deploy. ``lab status --json`` returns it like any other instance (name
    matches the ``attackbox`` aliases), so we learn it from the same read.
    """
    for inst in instances:
        name = str(inst.get("name") or "").lower()
        if any(a in name for a in _ALIASES["attackbox"]):
            return inst.get("id")
    return None


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
            # No live instance → clear stale cloud fields, mark absent.
            h["status"] = "absent"
            h["ip_private"] = None
            h["cloud_id"] = None
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
    ``last_checked_at``, and return an error check_run payload. The session's
    lifecycle status is owned by command execution and left unchanged — a
    failed *read* is not a range error.
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
        # A failed read is not a range error — don't clobber the lifecycle
        # status the command set. Stale last_checked_at + this payload signal it.
        return {"error": str(exc)}

    # Inventory sync: learn the attack box id post-deploy so /score can fetch
    # the report (§5.2). Persist only on change to avoid needless writes.
    box = find_attack_box(instances)
    snap = session.get("snapshot") or {}
    if box and snap.get("attack_box") != box:
        snap["attack_box"] = box
        session["snapshot"] = snap
        await db.upsert_session(session)

    updated = map_range_status(rng, instances)
    payload = summarize_changes(rng, updated)
    await db.upsert_range(session_id, updated)
    return payload


def parse_health_report(output: str) -> dict[str, t.Any] | None:
    """Extract the ``health-check --json`` report from possibly-noisy output.

    ``/health`` streams via ``start_command`` (stdout+stderr merged), and
    ``requireInfra`` prints a "credentials OK" line to stdout *before* the JSON,
    so the report is usually surrounded by log lines. Try the whole string, then
    the first ``{`` … last ``}`` span. Returns the report dict, or None.
    """
    candidates = [output]
    start, end = output.find("{"), output.rfind("}")
    if 0 <= start < end:
        candidates.append(output[start : end + 1])
    for candidate in candidates:
        try:
            parsed = json.loads(candidate)
        except (ValueError, TypeError):
            continue
        if isinstance(parsed, dict) and "checks" in parsed:
            return parsed
    return None


def host_health_from_report(checks: list[dict[str, t.Any]]) -> dict[str, str]:
    """Aggregate per-check results into a per-host verdict, keyed by UPPER host.

    A host is ``unhealthy`` if any of its checks FAILed, ``healthy`` if at least
    one passed and none failed, else ``unknown`` (only SKIPs). Checks carry the
    DC/server role in ``host`` (e.g. ``DC01``); hosts with no checks are absent
    from the map and left untouched by the overlay.
    """
    statuses: dict[str, set[str]] = {}
    for c in checks:
        host = str(c.get("host") or "").upper()
        if host:
            statuses.setdefault(host, set()).add(str(c.get("status") or ""))
    verdicts: dict[str, str] = {}
    for host, seen in statuses.items():
        if "FAIL" in seen:
            verdicts[host] = "unhealthy"
        elif "OK" in seen:
            verdicts[host] = "healthy"
        else:
            verdicts[host] = "unknown"
    return verdicts


async def apply_health(
    app: t.Any, session_id: str, output: str, exit_code: int
) -> dict[str, t.Any] | None:
    """Overlay /health results onto the range (§6.4 two write paths).

    Prefers per-host verdicts parsed from ``health-check --json``; falls back to
    a range-level verdict from the exit code when the output isn't a JSON report
    (e.g. an older CLI, or a run that failed before emitting one). Returns the
    parsed report (so the caller can surface it in chat), or None on fallback.
    """
    db = app.state.db
    rng = await db.get_range(session_id)
    if rng is None:
        return None

    report = parse_health_report(output)

    if report is not None:
        per_host = host_health_from_report(report.get("checks") or [])
        for h in rng.get("hosts", []):
            key = str(h.get("id") or "").upper()
            hostname = str(h.get("hostname") or "").upper()
            verdict = per_host.get(key) or per_host.get(hostname)
            if verdict is not None:
                h["health"] = verdict
    else:
        verdict = "healthy" if exit_code == 0 else "unhealthy"
        for h in rng.get("hosts", []):
            if h.get("source") == "config":
                h["health"] = verdict
    await db.upsert_range(session_id, rng)
    return report


# Most extension machines are Linux (elk/wazuh/guacamole/lx01); ws01/exchange
# are the exceptions, but the node still appears — role/icon is secondary.
_EXT_ROLE = "linux"


async def _extension_nodes(session: dict[str, t.Any]) -> list[dict[str, t.Any]]:
    """Nodes for the range's **enabled** extensions, via `extension list --json`.

    This is how extension machines (ELK, Wazuh, …) reach the RangeView —
    ``seed_topology`` only knows config + infra nodes (§6.3).
    """
    anchor = session.get("anchor", {})
    cfg_path, env = anchor.get("config_path"), anchor.get("env")
    if not cfg_path or not env:
        return []
    argv = [
        commands.resolve_bin(str(paths.repo_root())),
        "--config",
        str(cfg_path),
        "--env",
        str(env),
        "extension",
        "list",
        "--json",
    ]
    rc, out, _err = await capture(argv, cwd=str(paths.repo_root()))
    if rc != 0:
        return []
    try:
        exts = json.loads(out)
    except Exception:  # noqa: BLE001
        return []
    nodes: list[dict[str, t.Any]] = []
    for ext in exts:
        if not ext.get("enabled"):
            continue
        for machine in ext.get("machines") or []:
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


async def reseed(app: t.Any, session_id: str) -> None:
    """Re-seed topology after /extensions or /variant change the node set (§6.3).

    Node set = config hosts + infra nodes (``seed_topology``) + enabled
    extension machines (``_extension_nodes``); ``merge_reseed`` preserves live
    state + layout for survivors.
    """
    db = app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        return
    snap = session.get("snapshot", {})
    cfg = labconfig.lab_config_path(str(paths.repo_root()), snap.get("lab"))
    seeded = labconfig.seed_topology(cfg, snap.get("provider"))

    existing_ids = {h["id"] for h in seeded["hosts"]}
    for node in await _extension_nodes(session):
        if node["id"] not in existing_ids:
            seeded["hosts"].append(node)
            existing_ids.add(node["id"])

    await db.upsert_range(session_id, labconfig.merge_reseed(rng, seeded))
