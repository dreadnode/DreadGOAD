"""Tests for the ingestion hook mapping (Phase 4: T4.1).

Standalone:  python console/backend/tests/test_hook.py
"""

from __future__ import annotations

import asyncio
import json
import pathlib
import sys
import tempfile
import types

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import health_sync, inventory_sync, topology_sync  # noqa: E402
from console.backend.db import Database  # noqa: E402
from console.backend.inventory_sync import (  # noqa: E402
    map_range_status,
    run_check,
    summarize_changes,
)


def _range() -> dict:
    def host(hid, role, source="config"):
        return {
            "id": hid,
            "hostname": hid,
            "role": role,
            "source": source,
            "status": "unknown",
            "health": "unknown",
            "ip_private": None,
            "ip_public": None,
            "cloud_id": None,
            "last_checked_at": None,
        }

    return {
        "session_id": "s-1",
        "hosts": [
            host("kingslanding", "dc"),
            host("winterfell", "dc"),
            host("attackbox", "attackbox", "infra"),
        ],
        "edges": [],
        "layout": {},
        "last_checked_at": None,
    }


def _variant_range() -> dict:
    """A variant range: hosts renamed (dc01 → solar), VMs still named by role."""

    def host(key, hostname, role, source="config"):
        return {
            "id": hostname,
            "key": key,
            "hostname": hostname,
            "role": role,
            "source": source,
            "status": "unknown",
            "health": "unknown",
            "ip_private": None,
            "ip_public": None,
            "cloud_id": None,
            "last_checked_at": None,
        }

    return {
        "session_id": "s-v",
        "hosts": [
            host("dc01", "solar", "dc"),
            host("dc02", "quantum-web", "dc"),
            host("srv03", "quantum", "member"),
            host("attackbox", "attackbox", "attackbox", "infra"),
        ],
        "edges": [],
        "layout": {},
        "last_checked_at": None,
    }


_VARIANT_INSTANCES = [
    {
        "name": "dreadindex-dreadgoad-dreadgoad-DC01-vm",
        "id": "az-dc01",
        "state": "running",
        "private_ip": "10.1.1.5",
    },
    {
        "name": "dreadindex-dreadgoad-dreadgoad-DC02-vm",
        "id": "az-dc02",
        "state": "running",
        "private_ip": "10.1.1.7",
    },
    {
        "name": "dreadindex-dreadgoad-dreadgoad-SRV03-vm",
        "id": "az-srv03",
        "state": "running",
        "private_ip": "10.1.1.4",
    },
    {
        "name": "dreadindex-dreadgoad-kali-vm",
        "id": "az-kali",
        "state": "running",
        "private_ip": "10.1.4.4",
    },
]


def test_variant_hosts_correlate_by_role_key() -> None:
    """Regression: renamed hosts were all marked absent though their VMs ran."""
    out = map_range_status(_variant_range(), _VARIANT_INSTANCES, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    assert hosts["solar"]["status"] == "running", hosts["solar"]
    assert hosts["solar"]["cloud_id"] == "az-dc01", hosts["solar"]
    assert hosts["solar"]["ip_private"] == "10.1.1.5", hosts["solar"]
    assert hosts["quantum-web"]["cloud_id"] == "az-dc02", hosts["quantum-web"]
    assert hosts["attackbox"]["cloud_id"] == "az-kali", "alias must still work"
    assert not any(h["status"] == "absent" for h in out["hosts"]), out["hosts"]
    print("PASS test_variant_hosts_correlate_by_role_key")


def test_hostname_substring_collision_does_not_mismatch() -> None:
    """'quantum' ⊂ 'quantum-web': keying on the role avoids the wrong VM."""
    out = map_range_status(_variant_range(), _VARIANT_INSTANCES, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    # srv03's hostname is "quantum"; matching on hostname could have grabbed the
    # DC02 instance. The role key srv03 pins it to the right one.
    assert hosts["quantum"]["cloud_id"] == "az-srv03", hosts["quantum"]
    print("PASS test_hostname_substring_collision_does_not_mismatch")


def test_parse_cloud_account_from_arm_ids() -> None:
    """Subscription + resource group come free from the instance ids we already read."""
    got = inventory_sync.parse_cloud_account(_VARIANT_INSTANCES)
    assert got == {}, "fixture ids are opaque, so nothing should be claimed"

    arm = [
        {"id": "i-0abc", "name": "aws-box"},  # AWS first — must not stop the scan
        {
            "id": "/subscriptions/70a9c8a4-6bc6-4a48-ae24-27996cea8c02/resourceGroups/"
            "DREADINDEX-DREADGOAD-RG/providers/Microsoft.Compute/virtualMachines/x",
            "name": "x",
        },
    ]
    assert inventory_sync.parse_cloud_account(arm) == {
        "account": "70a9c8a4-6bc6-4a48-ae24-27996cea8c02",
        "group": "DREADINDEX-DREADGOAD-RG",
    }
    # Azure is case-inconsistent about the segment: both forms appear in real ids.
    lower = [{"id": "/subscriptions/S1/resourcegroups/RG1/providers/x"}]
    assert inventory_sync.parse_cloud_account(lower) == {
        "account": "S1",
        "group": "RG1",
    }
    # Nothing to claim → empty, never a partial or bogus result.
    assert inventory_sync.parse_cloud_account([]) == {}
    assert inventory_sync.parse_cloud_account([{"id": None}]) == {}
    assert inventory_sync.parse_cloud_account([{"id": "i-0abc123"}]) == {}
    assert (
        inventory_sync.parse_cloud_account([{"id": "/subscriptions/only-a-sub"}]) == {}
    )
    print("PASS test_parse_cloud_account_from_arm_ids")


def test_parse_cloud_account_prefers_cli_fields() -> None:
    """The CLI's account/group fields win over parsing the resource id.

    They're authoritative, and they're the only source for an AWS account id —
    `i-0abc…` encodes nothing. Parsing stays as a fallback so a range read by an
    older binary still resolves.
    """
    # AWS: account only, no group (AWS has no resource-group equivalent).
    aws = [{"id": "i-0abc", "name": "x", "account": "123456789012"}]
    # Provider-neutral key: an AWS account must NOT be filed as an Azure
    # subscription. Reachable only since the CLI began reporting `account`.
    assert inventory_sync.parse_cloud_account(aws) == {"account": "123456789012"}
    assert "resource_group" not in inventory_sync.parse_cloud_account(aws), (
        "AWS has no group"
    )

    # Azure via the reported fields.
    azure = [
        {
            "id": "/subscriptions/S/resourceGroups/G/providers/x",
            "account": "SUB-FIELD",
            "group": "RG-FIELD",
        }
    ]
    assert inventory_sync.parse_cloud_account(azure) == {
        "account": "SUB-FIELD",
        "group": "RG-FIELD",
    }, "reported fields must win over the id"

    # Older CLI: no fields at all → fall back to the id.
    legacy = [{"id": "/subscriptions/S1/resourceGroups/G1/providers/x"}]
    assert inventory_sync.parse_cloud_account(legacy) == {
        "account": "S1",
        "group": "G1",
    }

    # Blank/whitespace fields are not "reported" — fall through, don't claim "".
    blank = [
        {
            "id": "/subscriptions/S2/resourceGroups/G2/providers/x",
            "account": "  ",
            "group": "",
        }
    ]
    assert inventory_sync.parse_cloud_account(blank) == {
        "account": "S2",
        "group": "G2",
    }, "whitespace must not shadow the id fallback"
    print("PASS test_parse_cloud_account_prefers_cli_fields")


def test_backfill_keys_adds_field_without_touching_node_set() -> None:
    rng = _range()  # pre-key doc: no "key" on any host
    seeded = {
        "hosts": [
            {"id": "kingslanding", "key": "dc01"},
            {"id": "winterfell", "key": "dc02"},
            {"id": "attackbox", "key": "attackbox"},
        ]
    }
    before = [h["id"] for h in rng["hosts"]]
    assert inventory_sync.backfill_keys(rng, seeded) is True
    assert [h["id"] for h in rng["hosts"]] == before, "node set must not change"
    assert {h["id"]: h["key"] for h in rng["hosts"]} == {
        "kingslanding": "dc01",
        "winterfell": "dc02",
        "attackbox": "attackbox",
    }
    # Idempotent, and unknown ids fall back to the id itself.
    assert inventory_sync.backfill_keys(rng, seeded) is False
    orphan = {"hosts": [{"id": "elk", "hostname": "elk"}]}
    inventory_sync.backfill_keys(orphan, seeded)
    assert orphan["hosts"][0]["key"] == "elk"
    # An empty seed (lab config absent) must not drop or blank anything.
    r2 = _range()
    inventory_sync.backfill_keys(r2, {"hosts": []})
    assert [h["id"] for h in r2["hosts"]] == before
    assert r2["hosts"][0]["key"] == "kingslanding"
    print("PASS test_backfill_keys_adds_field_without_touching_node_set")


def test_health_overlay_matches_by_role_key() -> None:
    """Per-host health is keyed by role (DC01), not the variant hostname."""
    checks = [
        {"name": "AD DC", "host": "DC01", "status": "OK", "detail": ""},
        {"name": "AD DC", "host": "DC02", "status": "FAIL", "detail": "down"},
    ]
    verdicts = health_sync.host_health_from_report(checks)
    rng = _variant_range()
    for h in rng["hosts"]:
        role = str(h.get("key") or "").upper()
        if role in verdicts:
            h["health"] = verdicts[role]
    hosts = {h["id"]: h for h in rng["hosts"]}
    assert hosts["solar"]["health"] == "healthy", hosts["solar"]
    assert hosts["quantum-web"]["health"] == "unhealthy", hosts["quantum-web"]
    print("PASS test_health_overlay_matches_by_role_key")


def test_matched_host_gets_state_ip_id() -> None:
    instances = [
        {
            "name": "goad-dreadgoad-kingslanding-vm",
            "id": "i-0abc",
            "state": "running",
            "private_ip": "10.0.4.124",
        },
    ]
    out = map_range_status(_range(), instances, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    k = hosts["kingslanding"]
    assert k["status"] == "running", k
    assert k["ip_private"] == "10.0.4.124" and k["cloud_id"] == "i-0abc", k
    assert k["last_checked_at"] == "T", k
    print("PASS test_matched_host_gets_state_ip_id")


def test_unmatched_config_host_is_absent() -> None:
    r = _range()
    # Give winterfell a stale IP/cloud_id to prove they're cleared when absent.
    for h in r["hosts"]:
        if h["id"] == "winterfell":
            h["ip_private"] = "10.9.9.9"
            h["cloud_id"] = "i-old"
    instances = [
        {
            "name": "x-kingslanding-vm",
            "id": "i-1",
            "state": "running",
            "private_ip": "1.2.3.4",
        }
    ]
    out = map_range_status(r, instances, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    assert hosts["winterfell"]["status"] == "absent", "no instance → absent"
    assert (
        hosts["winterfell"]["ip_private"] is None
        and hosts["winterfell"]["cloud_id"] is None
    ), "absent must clear stale ip/cloud_id"
    print("PASS test_unmatched_config_host_is_absent")


def test_unmatched_instance_ignored_no_new_node() -> None:
    instances = [
        {
            "name": "some-random-elk-vm",
            "id": "i-9",
            "state": "running",
            "private_ip": "9.9.9.9",
        }
    ]
    out = map_range_status(_range(), instances, now="T")
    assert len(out["hosts"]) == 3, "hook must not invent nodes"
    print("PASS test_unmatched_instance_ignored_no_new_node")


def test_infra_alias_matches_kali() -> None:
    instances = [
        {
            "name": "goad-dreadgoad-kali",
            "id": "i-kali",
            "state": "running",
            "private_ip": "10.0.4.9",
        }
    ]
    out = map_range_status(_range(), instances, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    assert hosts["attackbox"]["status"] == "running", (
        "attackbox should match the kali VM"
    )
    assert hosts["attackbox"]["cloud_id"] == "i-kali", hosts["attackbox"]
    print("PASS test_infra_alias_matches_kali")


def test_state_normalization() -> None:
    for cloud, expected in [
        ("running", "running"),
        ("stopped", "stopped"),
        ("deallocated", "stopped"),
        ("pending", "provisioning"),
        ("terminated", "absent"),
        ("weird", "unknown"),
    ]:
        instances = [
            {"name": "x-kingslanding-y", "id": "i", "state": cloud, "private_ip": ""}
        ]
        out = map_range_status(_range(), instances, now="T")
        st = {h["id"]: h for h in out["hosts"]}["kingslanding"]["status"]
        assert st == expected, f"{cloud} → {st}, expected {expected}"
    print("PASS test_state_normalization")


def test_summarize_changes() -> None:
    before = _range()
    after = map_range_status(
        before,
        [
            {
                "name": "x-kingslanding-y",
                "id": "i",
                "state": "running",
                "private_ip": "",
            },
        ],
        now="T",
    )
    diff = summarize_changes(before, after)
    # kingslanding unknown→running, winterfell unknown→absent, attackbox unknown→absent
    assert diff["hosts_updated"] == 3, diff
    ids = {c["id"] for c in diff["changes"]}
    assert "kingslanding" in ids, diff
    print("PASS test_summarize_changes")


async def _db_with_session() -> Database:
    tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
    tmp.close()
    db = await Database(tmp.name).connect()
    await db.upsert_session(
        {
            "id": "s-1",
            "status": "running",  # a just-succeeded command's lifecycle status
            "anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"},
            "snapshot": {"provider": "aws", "lab": None},
        }
    )
    await db.upsert_range(
        "s-1",
        {
            "session_id": "s-1",
            "hosts": [
                {
                    "id": "kingslanding",
                    "hostname": "kingslanding",
                    "role": "dc",
                    "source": "config",
                    "status": "unknown",
                    "health": "unknown",
                    "ip_private": None,
                    "ip_public": None,
                    "cloud_id": None,
                    "last_checked_at": "OLD",
                }
            ],
            "edges": [],
            "layout": {},
            "last_checked_at": "OLD",
        },
    )
    return db


class _App:
    def __init__(self, db: Database) -> None:
        self.state = types.SimpleNamespace(db=db, sessions=None)


async def test_run_check_success_updates_range() -> None:
    db = await _db_with_session()
    orig = inventory_sync.capture

    async def fake_capture(argv, cwd):  # noqa: ANN001
        payload = [
            {
                "name": "goad-dreadgoad-kingslanding-vm",
                "id": "i-1",
                "state": "running",
                "private_ip": "10.0.0.5",
            }
        ]
        return (0, json.dumps(payload), "")

    inventory_sync.capture = fake_capture
    try:
        result = await run_check(_App(db), "s-1")
        assert "error" not in result, result
        # Integration-specific: the overlay was persisted and the timestamp
        # advanced. (Field-level mapping is covered by the pure map tests.)
        rng = await db.get_range("s-1")
        assert rng is not None
        assert {h["id"]: h for h in rng["hosts"]}["kingslanding"]["status"] == "running"
        assert rng["last_checked_at"] != "OLD", "success should advance last_checked_at"
        print("PASS test_run_check_success_updates_range")
    finally:
        inventory_sync.capture = orig
        await db.close()


async def test_run_check_syncs_attack_box() -> None:
    """run_check learns the attack box id from the instances and persists it (§5.2)."""
    db = await _db_with_session()
    orig = inventory_sync.capture

    async def fake_capture(argv, cwd):  # noqa: ANN001
        payload = [
            {
                "name": "goad-dreadgoad-kingslanding-vm",
                "id": "i-dc",
                "state": "running",
            },
            {
                "name": "goad-dreadgoad-kali-attackbox",
                "id": "i-kali",
                "state": "running",
            },
        ]
        return (0, json.dumps(payload), "")

    inventory_sync.capture = fake_capture
    try:
        # precondition: attack_box unknown
        s0 = await db.get_session("s-1")
        assert s0 is not None and s0["snapshot"].get("attack_box") is None

        await run_check(_App(db), "s-1")

        s1 = await db.get_session("s-1")
        assert s1 is not None
        assert s1["snapshot"]["attack_box"] == "i-kali", s1["snapshot"]
        print("PASS test_run_check_syncs_attack_box")
    finally:
        inventory_sync.capture = orig
        await db.close()


async def test_run_check_failure_preserves_state() -> None:
    db = await _db_with_session()
    orig = inventory_sync.capture

    async def fake_capture(argv, cwd):  # noqa: ANN001
        return (1, "", "boom: creds expired")

    inventory_sync.capture = fake_capture
    try:
        result = await run_check(_App(db), "s-1")
        assert "error" in result, "failed read should return an error payload"

        rng = await db.get_range("s-1")
        assert rng is not None
        assert rng["last_checked_at"] == "OLD", "failure must NOT advance (stale)"
        k = {h["id"]: h for h in rng["hosts"]}["kingslanding"]
        assert k["status"] == "unknown", "host state must stay stale on failure"

        # F1: a failed read must not clobber the session's lifecycle status.
        s = await db.get_session("s-1")
        assert s is not None and s["status"] == "running", (
            f"read failure must not set session error, got {s['status'] if s else None}"
        )
        print("PASS test_run_check_failure_preserves_state")
    finally:
        inventory_sync.capture = orig
        await db.close()


async def test_apply_health_targets_config_hosts() -> None:
    tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
    tmp.close()
    db = await Database(tmp.name).connect()
    try:
        await db.upsert_range(
            "s",
            {
                "session_id": "s",
                "hosts": [
                    {"id": "dc", "source": "config", "health": "unknown"},
                    {"id": "attackbox", "source": "infra", "health": "unknown"},
                ],
                "edges": [],
                "layout": {},
            },
        )
        # Fallback path: output isn't a JSON report → range-level verdict from
        # exit code, applied to config hosts only.
        await health_sync.apply_health(_App(db), "s", "human-readable table", 0)
        rng = await db.get_range("s")
        assert rng is not None
        hosts = {h["id"]: h for h in rng["hosts"]}
        assert hosts["dc"]["health"] == "healthy", hosts
        assert hosts["attackbox"]["health"] == "unknown", "infra host must be untouched"

        await health_sync.apply_health(_App(db), "s", "", 1)
        rng = await db.get_range("s")
        assert rng is not None
        hosts = {h["id"]: h for h in rng["hosts"]}
        assert hosts["dc"]["health"] == "unhealthy", hosts
        print("PASS test_apply_health_targets_config_hosts")
    finally:
        await db.close()


def test_parse_health_report_noisy() -> None:
    """Report parses from NDJSON: per-check lines + noise, report is the line w/ 'checks'."""
    # NDJSON stream: credentials prefix, per-check lines, then the report line.
    lines = [
        "  \x1b[32maws credentials OK (arn:aws:iam::123:user/x)\x1b[0m",
        json.dumps(
            {"name": "DC01 DC", "host": "DC01", "status": "OK", "detail": "DC01"}
        ),
        json.dumps(
            {"name": "DC02 Trust", "host": "DC02", "status": "FAIL", "detail": "x"}
        ),
        json.dumps(
            {
                "passed": 1,
                "failed": 1,
                "skipped": 0,
                "checks": [{"host": "DC01", "status": "OK"}],
            }
        ),
        "some trailing log",
    ]
    parsed = health_sync.parse_health_report("\n".join(lines))
    assert parsed is not None, "must find the report line among NDJSON + noise"
    assert parsed["passed"] == 1 and parsed["checks"][0]["host"] == "DC01", parsed
    # Fallback: an older single indented blob still parses.
    blob = "creds ok\n" + json.dumps(
        {"checks": [{"host": "DC1", "status": "OK"}]}, indent=2
    )
    assert health_sync.parse_health_report(blob) is not None, (
        "fallback for non-NDJSON output"
    )
    # genuinely non-JSON → None
    assert health_sync.parse_health_report("no json here") is None
    # JSON lines that aren't a report (no 'checks') → None
    assert health_sync.parse_health_report('{"foo": 1}\n{"status": "OK"}') is None
    print("PASS test_parse_health_report_noisy")


def test_host_health_from_report() -> None:
    """Per-check → per-host aggregation (any FAIL→unhealthy; else OK→healthy)."""
    checks = [
        {"name": "DC01 DC", "host": "DC01", "status": "OK"},
        {"name": "DC01 Repl", "host": "DC01", "status": "OK"},
        {"name": "DC02 DC", "host": "DC02", "status": "OK"},
        {"name": "DC02 Trust", "host": "DC02", "status": "FAIL"},
        {"name": "SRV01 MSSQL", "host": "SRV01", "status": "SKIP"},
    ]
    v = health_sync.host_health_from_report(checks)
    assert v == {"DC01": "healthy", "DC02": "unhealthy", "SRV01": "unknown"}, v
    assert health_sync.host_health_from_report([]) == {}, "no checks → empty map"
    print("PASS test_host_health_from_report")


async def test_apply_health_per_host_from_json() -> None:
    """A JSON report sets per-host health (matched case-insensitively)."""
    tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
    tmp.close()
    db = await Database(tmp.name).connect()
    try:
        await db.upsert_range(
            "s",
            {
                "session_id": "s",
                "hosts": [
                    {
                        "id": "dc01",
                        "hostname": "dc01",
                        "source": "config",
                        "health": "unknown",
                    },
                    {
                        "id": "srv02",
                        "hostname": "srv02",
                        "source": "config",
                        "health": "unknown",
                    },
                    {"id": "attackbox", "source": "infra", "health": "unknown"},
                ],
                "edges": [],
                "layout": {},
            },
        )
        report = json.dumps(
            {
                "passed": 1,
                "failed": 1,
                "skipped": 0,
                "checks": [
                    {"name": "DC01 DC", "host": "DC01", "status": "OK"},
                    {"name": "SRV02 Membership", "host": "SRV02", "status": "FAIL"},
                ],
            }
        )
        # exit_code=1 (a check failed) but the JSON drives per-host, not exit code
        await health_sync.apply_health(_App(db), "s", report, 1)
        rng = await db.get_range("s")
        assert rng is not None
        hosts = {h["id"]: h for h in rng["hosts"]}
        assert hosts["dc01"]["health"] == "healthy", (
            hosts
        )  # matched DC01 case-insensitively
        assert hosts["srv02"]["health"] == "unhealthy", hosts
        assert hosts["attackbox"]["health"] == "unknown", (
            "host with no checks untouched"
        )
        print("PASS test_apply_health_per_host_from_json")
    finally:
        await db.close()


async def test_reseed_adds_enabled_extension_nodes() -> None:
    tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
    tmp.close()
    db = await Database(tmp.name).connect()
    await db.upsert_session(
        {
            "id": "s",
            "anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"},
            "snapshot": {"provider": "aws", "lab": "ad/DOES-NOT-EXIST"},
        }
    )
    await db.upsert_range(
        "s",
        {
            "session_id": "s",
            "hosts": [
                {
                    "id": "attackbox",
                    "hostname": "attackbox",
                    "role": "attackbox",
                    "source": "infra",
                    "status": "running",
                    "health": "unknown",
                    "ip_private": "1.2.3.4",
                    "ip_public": None,
                    "cloud_id": "i-x",
                    "last_checked_at": "T",
                }
            ],
            "edges": [],
            "layout": {"attackbox": {"x": 1, "y": 2}},
        },
    )
    orig = topology_sync.capture

    async def fake_capture(argv, cwd):  # noqa: ANN001
        payload = [
            {"name": "elk", "enabled": True, "machines": ["elk"]},
            {"name": "exchange", "enabled": False, "machines": ["srv01"]},
        ]
        return (0, json.dumps(payload), "")

    topology_sync.capture = fake_capture
    try:
        await topology_sync.reseed(_App(db), "s")
        rng = await db.get_range("s")
        assert rng is not None
        hosts = {h["id"]: h for h in rng["hosts"]}
        # enabled extension → node added, tagged source=extension
        assert "elk" in hosts and hosts["elk"]["source"] == "extension", hosts
        # disabled extension → not added
        assert "srv01" not in hosts, "disabled extension must not appear"
        # surviving infra node keeps live state + layout
        assert hosts["attackbox"]["status"] == "running", hosts["attackbox"]
        assert "attackbox" in rng["layout"], "layout must be preserved"
        print("PASS test_reseed_adds_enabled_extension_nodes")
    finally:
        topology_sync.capture = orig
        await db.close()


def main() -> None:
    test_variant_hosts_correlate_by_role_key()
    test_hostname_substring_collision_does_not_mismatch()
    test_parse_cloud_account_from_arm_ids()
    test_parse_cloud_account_prefers_cli_fields()
    test_backfill_keys_adds_field_without_touching_node_set()
    test_health_overlay_matches_by_role_key()
    test_matched_host_gets_state_ip_id()
    test_unmatched_config_host_is_absent()
    test_unmatched_instance_ignored_no_new_node()
    test_infra_alias_matches_kali()
    test_state_normalization()
    test_summarize_changes()
    asyncio.run(test_run_check_success_updates_range())
    asyncio.run(test_run_check_syncs_attack_box())
    asyncio.run(test_run_check_failure_preserves_state())
    test_parse_health_report_noisy()
    test_host_health_from_report()
    asyncio.run(test_apply_health_targets_config_hosts())
    asyncio.run(test_apply_health_per_host_from_json())
    asyncio.run(test_reseed_adds_enabled_extension_nodes())
    print("ALL PASS")


if __name__ == "__main__":
    main()
