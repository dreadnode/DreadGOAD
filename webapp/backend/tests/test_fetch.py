"""Tests for /score report-fetch argv construction (Phase 7: F2).

The fetch now drives ``dreadgoad score fetch`` (which owns the SSM/Bastion
transfer + key discovery); the live transfer needs cloud and is verified
manually. Here we test the argv the web app builds + provider dispatch.

Standalone:  python webapp/backend/tests/test_fetch.py
"""

from __future__ import annotations

import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend.fetch import build_fetch_argv, local_report_path  # noqa: E402


def _session(snapshot: dict) -> dict:
    return {
        "anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"},
        "snapshot": snapshot,
    }


def _argv(
    snapshot: dict, remote="/root/report.jsonl", local="/s/report.jsonl"
) -> list[str]:
    return build_fetch_argv(_session(snapshot), remote, local, repo_root="/repo")


def test_argv_injects_anchor_and_verb() -> None:
    a = _argv({"provider": "aws", "attack_box": "i-0abc"})
    assert a[0].endswith("dreadgoad"), a
    assert a[1:5] == ["--config", "/x/dreadgoad.yaml", "--env", "dev"], a
    assert a[5:11] == [
        "score",
        "fetch",
        "--remote",
        "/root/report.jsonl",
        "--local",
        "/s/report.jsonl",
    ], a
    print("PASS test_argv_injects_anchor_and_verb")


def test_aws_passes_attack_box_and_region() -> None:
    a = _argv({"provider": "aws", "region": "us-west-2", "attack_box": "i-0abc"})
    assert "--attack-box" in a and "i-0abc" in a, a
    assert "--region" in a and "us-west-2" in a, a
    print("PASS test_aws_passes_attack_box_and_region")


def test_azure_auto_discovers_no_attack_box() -> None:
    # No key in snapshot → CLI auto-discovers both VM and key over Bastion.
    a = _argv({"provider": "azure", "azure": {}})
    assert "--attack-box" not in a, "azure must not pin an explicit resource id"
    assert "--ssh-key" not in a, a
    # An explicit key (if the snapshot ever carries one) is forwarded.
    a2 = _argv({"provider": "azure", "azure": {"ssh_key": "/keys/id"}})
    assert "--ssh-key" in a2 and "/keys/id" in a2, a2
    assert "--attack-box" not in a2, a2
    print("PASS test_azure_auto_discovers_no_attack_box")


def test_missing_or_unsupported_raises() -> None:
    for snap in (
        {"provider": "aws"},  # no attack_box
        {"provider": "proxmox", "attack_box": "x"},  # unsupported
    ):
        try:
            _argv(snap)
            raise AssertionError(f"expected ValueError for {snap}")
        except ValueError:
            pass
    print("PASS test_missing_or_unsupported_raises")


def test_local_report_path() -> None:
    assert local_report_path("/s", "/home/kali/report.jsonl").endswith("/report.jsonl")
    assert local_report_path("/s", "").endswith("report.jsonl")
    print("PASS test_local_report_path")


if __name__ == "__main__":
    test_argv_injects_anchor_and_verb()
    test_aws_passes_attack_box_and_region()
    test_azure_auto_discovers_no_attack_box()
    test_missing_or_unsupported_raises()
    test_local_report_path()
    print("ALL PASS")
