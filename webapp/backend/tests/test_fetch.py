"""Tests for /score report-fetch argv construction (Phase 7: F2).

The live scp/SSM transfer needs cloud + discovered creds (manual); the command
construction + provider dispatch are unit-tested here.

Standalone:  python webapp/backend/tests/test_fetch.py
"""

from __future__ import annotations

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend.fetch import build_fetch_argv, local_report_path  # noqa: E402


def test_aws_uses_ssm_ssh_proxy() -> None:
    snap = {"provider": "aws", "region": "us-west-2", "attack_box": "i-0abc"}
    argv = build_fetch_argv(snap, "/home/kali/report.jsonl", "/tmp/r.jsonl")
    assert argv[0] == "scp", argv
    assert any("ProxyCommand=" in a and "aws ssm start-session" in a for a in argv), (
        argv
    )
    assert any("--region us-west-2" in a for a in argv), argv
    assert "kali@i-0abc:/home/kali/report.jsonl" in argv, argv
    assert "/tmp/r.jsonl" in argv, argv
    print("PASS test_aws_uses_ssm_ssh_proxy")


def test_azure_uses_ssh_key() -> None:
    snap = {
        "provider": "azure",
        "attack_box": "kali",
        "azure": {"ssh_key": "/keys/id", "ssh_user": "kali"},
    }
    argv = build_fetch_argv(snap, "/home/kali/report.jsonl", "/tmp/r.jsonl")
    assert argv[0] == "scp" and "-i" in argv and "/keys/id" in argv, argv
    assert "kali@kali:/home/kali/report.jsonl" in argv, argv
    print("PASS test_azure_uses_ssh_key")


def test_missing_attack_box_or_key_raises() -> None:
    for snap in (
        {"provider": "aws"},  # no attack_box
        {"provider": "azure", "attack_box": "x", "azure": {}},  # no ssh_key
        {"provider": "proxmox", "attack_box": "x"},  # unsupported
    ):
        try:
            build_fetch_argv(snap, "r", "l")
            raise AssertionError(f"expected ValueError for {snap}")
        except ValueError:
            pass
    print("PASS test_missing_attack_box_or_key_raises")


def test_local_report_path() -> None:
    assert local_report_path("/s", "/home/kali/report.jsonl").endswith("/report.jsonl")
    assert local_report_path("/s", "").endswith("report.jsonl")
    print("PASS test_local_report_path")


if __name__ == "__main__":
    test_aws_uses_ssm_ssh_proxy()
    test_azure_uses_ssh_key()
    test_missing_attack_box_or_key_raises()
    test_local_report_path()
    print("ALL PASS")
