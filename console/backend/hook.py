"""Post-command ingestion hook (design §6.2, §6.4).

After a command, discover live instances (via ``lab status --json``) and
overlay their cloud state onto the range's config-seeded hosts. Overlay-only:
config hosts with no live instance go ``absent``; live instances with no
config host are ignored. The mapping is pure and unit-tested; ``run_check``
wires it to the CLI + DB.
"""

from __future__ import annotations

import json
import re
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


# An Azure ARM resource id embeds the subscription and resource group, so
# `lab status --json` already tells us which account the range lives in.
_ARM_ID_RE = re.compile(
    r"^/subscriptions/(?P<sub>[^/]+)/resourcegroups/(?P<rg>[^/]+)/", re.IGNORECASE
)


def parse_cloud_account(instances: list[dict[str, t.Any]]) -> dict[str, str]:
    """Which cloud account and resource group the range lives in (pure).

    Returns provider-neutral keys — ``account`` is an AWS account ID or an Azure
    subscription ID, ``group`` an Azure resource group (AWS has no equivalent).
    Neutral on purpose: filing an AWS account under an Azure-shaped key would
    mislabel it, and the CLI already reports one field for both providers.

    Prefers the ``account``/``group`` fields the CLI reports — authoritative,
    and the only way to learn an AWS account ID, whose instance ids
    (``i-0abc…``) encode nothing. Falls back to parsing an Azure resource id,
    which embeds both, so a range read by an older ``dreadgoad`` binary still
    resolves. Returns ``{}`` when neither yields anything.
    """
    for inst in instances:
        account = str(inst.get("account") or "").strip()
        group = str(inst.get("group") or "").strip()
        if account or group:
            found = {}
            if account:
                found["account"] = account
            if group:
                found["group"] = group
            return found

    # Fallback: an older CLI emits neither field, but an Azure resource id
    # still names both. Nothing equivalent exists for AWS.
    for inst in instances:
        match = _ARM_ID_RE.match(str(inst.get("id") or ""))
        if match:
            return {"account": match.group("sub"), "group": match.group("rg")}
    return {}


def _match(
    host: dict[str, t.Any], instances: list[dict[str, t.Any]]
) -> dict[str, t.Any] | None:
    """Find the instance whose name contains the host's role key (or an alias).

    Correlates on ``key`` — the config key / CLI host role (``dc01``) — because
    cloud VMs are named after the role (``…-dreadgoad-DC01-vm``), never after a
    variant's randomized hostname (``solar``). Matching on the hostname leaves
    every host in a variant range unmatched, and would also risk one hostname
    being a substring of another (``quantum`` ⊂ ``quantum-web``).

    Falls back to ``id`` for range docs seeded before ``key`` existed.

    Last match wins, mirroring the CLI's ``discoverHostMap``
    (cli/cmd/infra.go), so both pick the same VM when names are ambiguous.
    """
    hid = str(host.get("key") or host.get("id") or "").lower()
    aliases = _ALIASES.get(hid, [hid])
    found: dict[str, t.Any] | None = None
    for inst in instances:
        name = str(inst.get("name") or "").lower()
        if any(a in name for a in aliases):
            found = inst
    return found


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
            # The provider's VM name (…-dreadgoad-DC01-vm), distinct from the
            # in-guest hostname a variant randomizes (solar). Operators need it
            # to find the machine in the cloud console.
            h["cloud_name"] = inst.get("name") or h.get("cloud_name")
            # Read opportunistically: `lab status --json` does not emit a public
            # IP today, and these ranges assign none (assign_public_ip defaults
            # false — outbound via NAT, inbound via Bastion). Kept so the field
            # populates by itself if the CLI ever reports one.
            h["ip_public"] = inst.get("public_ip") or h.get("ip_public")
        h["last_checked_at"] = now
        hosts_out.append(h)
    out = dict(rng)
    out["hosts"] = hosts_out
    out["last_checked_at"] = now
    return out


def backfill_keys(rng: dict[str, t.Any], seeded: dict[str, t.Any]) -> bool:
    """Add the missing ``key`` field to hosts seeded before keys existed (pure).

    Ranges created earlier have no ``key``, so their hosts can't correlate to
    cloud instances. Copy the key from a freshly-seeded topology, matching on
    node id. Deliberately *not* a reseed: a reseed replaces the node set, which
    would drop every config host if the lab config isn't on disk. This only ever
    adds a field. Returns True if anything changed.
    """
    by_id = {h.get("id"): h.get("key") for h in seeded.get("hosts", [])}
    changed = False
    for h in rng.get("hosts", []):
        if "key" not in h:
            # Fall back to the id: correct for infra nodes, and no worse than
            # the old behavior for anything the seed doesn't know about.
            h["key"] = by_id.get(h.get("id")) or h.get("id")
            changed = True
    return changed


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

    # Self-heal ranges seeded before hosts carried their role key, so existing
    # sessions start correlating without needing to be recreated.
    if any("key" not in h for h in rng.get("hosts", [])):
        snap = session.get("snapshot") or {}
        cfg = labconfig.lab_config_path(str(paths.repo_root()), snap.get("lab"))
        if backfill_keys(rng, labconfig.seed_topology(cfg, snap.get("provider"))):
            await db.upsert_range(session_id, rng)

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

    # Inventory sync: learn what the read tells us about the range's location.
    # Both are discovered post-deploy, and both are persisted in one write, only
    # when something actually changed (§5.2).
    snap = session.get("snapshot") or {}
    dirty = False

    # The attack box id, so /score can fetch the report off it.
    box = find_attack_box(instances)
    if box and snap.get("attack_box") != box:
        snap["attack_box"] = box
        dirty = True

    # Which cloud account/resource group the range actually landed in — the
    # config never states it, so this is the only place it becomes knowable.
    # Stored at the top level, not inside the provider block: these are
    # range-identity facts like provider/region, and nesting an AWS account
    # under ``snapshot.azure`` would file it under the wrong provider's schema.
    account = parse_cloud_account(instances)
    for key, value in account.items():
        if snap.get(key) != value:
            snap[key] = value
            dirty = True

    if dirty:
        session["snapshot"] = snap
        await db.upsert_session(session)

    updated = map_range_status(rng, instances)
    payload = summarize_changes(rng, updated)
    await db.upsert_range(session_id, updated)
    return payload


def parse_health_report(output: str) -> dict[str, t.Any] | None:
    """Extract the ``health-check --json`` report from possibly-noisy output.

    ``--json`` streams NDJSON: one compact line per check, then a final line
    carrying a ``checks`` field (the report). Output is also surrounded by log
    lines (requireInfra's "credentials OK", merged stderr). Scan lines for the
    report; fall back to whole-string / first-``{``…last-``}`` for older
    (single-blob) output. Returns the report dict, or None.
    """
    for line in output.splitlines():
        line = line.strip()
        if not line.startswith("{") or '"checks"' not in line:
            continue
        try:
            parsed = json.loads(line)
        except (ValueError, TypeError):
            continue
        if isinstance(parsed, dict) and "checks" in parsed:
            return parsed
    # Fallback: whole string, then the first-{ … last-} span (non-NDJSON output).
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
            # Checks are keyed by host *role* (DC01), so correlate on ``key``
            # first — under a variant the id/hostname is the renamed host
            # (``solar``) and would never match. id/hostname stay as fallbacks
            # for pre-``key`` range docs.
            role = str(h.get("key") or "").upper()
            hid = str(h.get("id") or "").upper()
            hostname = str(h.get("hostname") or "").upper()
            verdict = per_host.get(role) or per_host.get(hid) or per_host.get(hostname)
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
