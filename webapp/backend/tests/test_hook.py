"""Tests for the ingestion hook mapping (Phase 4: T4.1).

Standalone:  python webapp/backend/tests/test_hook.py
"""

from __future__ import annotations

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend.hook import map_range_status, summarize_changes  # noqa: E402


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
    instances = [
        {
            "name": "x-kingslanding-vm",
            "id": "i-1",
            "state": "running",
            "private_ip": "1.2.3.4",
        }
    ]
    out = map_range_status(_range(), instances, now="T")
    hosts = {h["id"]: h for h in out["hosts"]}
    assert hosts["winterfell"]["status"] == "absent", "no instance → absent"
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


def main() -> None:
    test_matched_host_gets_state_ip_id()
    test_unmatched_config_host_is_absent()
    test_unmatched_instance_ignored_no_new_node()
    test_infra_alias_matches_kali()
    test_state_normalization()
    test_summarize_changes()
    print("ALL PASS")


if __name__ == "__main__":
    main()
