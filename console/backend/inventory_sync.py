"""Cloud instance discovery and range inventory synchronization."""

from __future__ import annotations

import json
import re
import typing as t
from datetime import datetime, timezone

from . import commands, labconfig, paths, projectroot
from .cli import capture

Capture = t.Callable[[list[str], str], t.Awaitable[tuple[int, str, str]]]

_STATE = {
    "running": "running",
    "stopped": "stopped",
    "deallocated": "stopped",
    "pending": "provisioning",
    "starting": "provisioning",
    "creating": "provisioning",
    "terminated": "absent",
}
_ALIASES = {
    "attackbox": ["attackbox", "kali", "attack"],
    "bastion": ["bastion"],
}
_ARM_ID_RE = re.compile(
    r"^/subscriptions/(?P<sub>[^/]+)/resourcegroups/(?P<rg>[^/]+)/", re.IGNORECASE
)


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def _norm_state(state: str | None) -> str:
    return _STATE.get((state or "").lower(), "unknown")


def find_attack_box(instances: list[dict[str, t.Any]]) -> str | None:
    """Return the attack box cloud id among live instances, if present."""
    for instance in instances:
        name = str(instance.get("name") or "").lower()
        if any(alias in name for alias in _ALIASES["attackbox"]):
            return instance.get("id")
    return None


def parse_cloud_account(instances: list[dict[str, t.Any]]) -> dict[str, str]:
    """Return provider-neutral account/group placement from instance data."""
    for instance in instances:
        account = str(instance.get("account") or "").strip()
        group = str(instance.get("group") or "").strip()
        if account or group:
            found = {}
            if account:
                found["account"] = account
            if group:
                found["group"] = group
            return found
    for instance in instances:
        match = _ARM_ID_RE.match(str(instance.get("id") or ""))
        if match:
            return {"account": match.group("sub"), "group": match.group("rg")}
    return {}


def _match(
    host: dict[str, t.Any], instances: list[dict[str, t.Any]]
) -> dict[str, t.Any] | None:
    """Match a host by config role key, with infra aliases as a fallback.

    Cloud VMs use config roles (for example ``DC01``), not variant hostnames.
    Last match wins to mirror the CLI's discovery behavior.
    """
    host_id = str(host.get("key") or host.get("id") or "").lower()
    aliases = _ALIASES.get(host_id, [host_id])
    found: dict[str, t.Any] | None = None
    for instance in instances:
        name = str(instance.get("name") or "").lower()
        if any(alias in name for alias in aliases):
            found = instance
    return found


def map_range_status(
    rng: dict[str, t.Any],
    instances: list[dict[str, t.Any]],
    now: str | None = None,
) -> dict[str, t.Any]:
    """Overlay cloud state without adding instances absent from the topology."""
    now = now or _now()
    hosts_out: list[dict[str, t.Any]] = []
    for host in rng.get("hosts", []):
        updated = dict(host)
        instance = _match(host, instances)
        if instance is None:
            updated["status"] = "absent"
            updated["ip_private"] = None
            updated["cloud_id"] = None
        else:
            updated["status"] = _norm_state(instance.get("state"))
            updated["ip_private"] = instance.get("private_ip") or updated.get(
                "ip_private"
            )
            updated["cloud_id"] = instance.get("id") or updated.get("cloud_id")
            updated["cloud_name"] = instance.get("name") or updated.get("cloud_name")
            updated["ip_public"] = instance.get("public_ip") or updated.get("ip_public")
        updated["last_checked_at"] = now
        hosts_out.append(updated)
    result = dict(rng)
    result["hosts"] = hosts_out
    result["last_checked_at"] = now
    return result


def backfill_keys(rng: dict[str, t.Any], seeded: dict[str, t.Any]) -> bool:
    """Add missing host role keys to pre-key range documents."""
    by_id = {host.get("id"): host.get("key") for host in seeded.get("hosts", [])}
    changed = False
    for host in rng.get("hosts", []):
        if "key" not in host:
            host["key"] = by_id.get(host.get("id")) or host.get("id")
            changed = True
    return changed


def summarize_changes(
    before: dict[str, t.Any], after: dict[str, t.Any]
) -> dict[str, t.Any]:
    """Build a check_run payload listing host status changes."""
    previous = {host["id"]: host.get("status") for host in before.get("hosts", [])}
    changes = []
    for host in after.get("hosts", []):
        old = previous.get(host["id"])
        if old != host.get("status"):
            changes.append({"id": host["id"], "from": old, "to": host.get("status")})
    return {"hosts_updated": len(changes), "changes": changes}


async def run_check(
    app: t.Any,
    session_id: str,
    capture_command: Capture | None = None,
) -> dict[str, t.Any]:
    """Discover live instances and synchronize session/range inventory.

    A failed inventory read leaves the existing range and its timestamp stale;
    it does not turn a successfully-running session into an error state.
    """
    db = app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        return {"error": "session/range not found"}

    if any("key" not in host for host in rng.get("hosts", [])):
        snapshot = session.get("snapshot") or {}
        config = labconfig.lab_config_path(str(paths.repo_root()), snapshot.get("lab"))
        seeded = labconfig.seed_topology(config, snapshot.get("provider"))
        if backfill_keys(rng, seeded):
            await db.upsert_range(session_id, rng)

    argv = commands.build_argv(session, "/instances", repo_root=str(paths.repo_root()))
    try:
        runner = capture_command or capture
        # Same tree as the commands whose results this records — see
        # projectroot.run_cwd. (lab_config_path above stays on repo_root: the
        # lab definitions are console-side, not part of the range's checkout.)
        return_code, stdout, stderr = await runner(
            argv, projectroot.run_cwd(session, paths.repo_root())
        )
        if return_code != 0:
            raise RuntimeError(
                f"lab status --json exited {return_code}: {stderr[-500:]}"
            )
        instances = json.loads(stdout)
    except Exception as exc:  # noqa: BLE001
        return {"error": str(exc)}

    snapshot = session.get("snapshot") or {}
    dirty = False
    attack_box = find_attack_box(instances)
    if attack_box and snapshot.get("attack_box") != attack_box:
        snapshot["attack_box"] = attack_box
        dirty = True
    for key, value in parse_cloud_account(instances).items():
        if snapshot.get(key) != value:
            snapshot[key] = value
            dirty = True
    if dirty:
        session["snapshot"] = snapshot
        await db.upsert_session(session)

    updated = map_range_status(rng, instances)
    payload = summarize_changes(rng, updated)
    await db.upsert_range(session_id, updated)
    return payload
