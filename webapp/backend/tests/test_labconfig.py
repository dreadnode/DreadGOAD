"""Tests for snapshot derivation, topology seeding, and yaml backup (Phase 2).

Standalone:  python webapp/backend/tests/test_labconfig.py
"""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend.labconfig import (  # noqa: E402
    backup_yaml,
    derive_snapshot,
    merge_reseed,
    seed_topology,
)

_REPO = pathlib.Path(__file__).resolve().parents[3]

_FIXTURE_YAML = """\
provider: azure
region: centralus
environments:
  staging:
    variant: true
    variant_source: ad/GOAD
    variant_target: ad/GOAD-dreadindex
    variant_name: dreadindex
    vpc_cidr: "10.1.0.0/16"
"""

_FIXTURE_YAML_AWS = """\
provider: aws
region: us-west-2
environments:
  dev:
    variant_source: ad/GOAD
    vpc_cidr: "10.0.0.0/16"
"""


def test_derive_snapshot_azure() -> None:
    tmp = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
    tmp.write(_FIXTURE_YAML)
    tmp.close()
    snap = derive_snapshot(tmp.name, "staging")
    assert snap["provider"] == "azure", snap
    assert snap["region"] == "centralus", snap
    assert snap["lab"] == "ad/GOAD-dreadindex", "lab should be variant_target"
    assert snap["variant_name"] == "dreadindex", snap
    assert snap["vpc_cidr"] == "10.1.0.0/16", snap
    assert snap["attack_box"] is None, "attack_box is discovered, not derived"
    assert "azure" in snap and snap["azure"]["ssh_user"] == "kali", snap
    assert "aws" not in snap, "no aws block for an azure session"
    os.unlink(tmp.name)
    print("PASS test_derive_snapshot_azure")


def test_derive_snapshot_aws_falls_back_to_source() -> None:
    tmp = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
    tmp.write(_FIXTURE_YAML_AWS)
    tmp.close()
    snap = derive_snapshot(tmp.name, "dev")
    assert snap["provider"] == "aws", snap
    # No variant_target → lab falls back to variant_source.
    assert snap["lab"] == "ad/GOAD", snap
    assert snap["aws"] == {"profile": None}, snap
    os.unlink(tmp.name)
    print("PASS test_derive_snapshot_aws_falls_back_to_source")


def test_derive_snapshot_unknown_env_raises() -> None:
    tmp = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
    tmp.write(_FIXTURE_YAML)
    tmp.close()
    try:
        derive_snapshot(tmp.name, "does-not-exist")
        raise AssertionError("expected ValueError for unknown env")
    except ValueError:
        pass
    finally:
        os.unlink(tmp.name)
    print("PASS test_derive_snapshot_unknown_env_raises")


def test_seed_topology_from_goad_config() -> None:
    cfg = _REPO / "ad" / "GOAD" / "data" / "config.json"
    assert cfg.is_file(), f"missing fixture: {cfg}"
    topo = seed_topology(str(cfg), provider="azure")
    hosts = {h["id"]: h for h in topo["hosts"]}

    # kingslanding is a DC seeded from config.
    assert "kingslanding" in hosts, hosts.keys()
    assert hosts["kingslanding"]["role"] == "dc", hosts["kingslanding"]
    assert hosts["kingslanding"]["source"] == "config", hosts["kingslanding"]
    # dynamic fields start blank/unknown.
    assert hosts["kingslanding"]["status"] == "unknown", hosts["kingslanding"]
    assert hosts["kingslanding"]["cloud_id"] is None, hosts["kingslanding"]

    # a `server` type maps to role member (castelblack = srv02).
    assert hosts["castelblack"]["role"] == "member", hosts["castelblack"]

    # infra nodes: attack box always; bastion for azure.
    assert hosts["attackbox"]["source"] == "infra", "attackbox missing"
    assert hosts["bastion"]["source"] == "infra", "azure should get a bastion node"

    # edges deferred in v1.
    assert topo["edges"] == [], "edges should be empty (deferred)"
    print("PASS test_seed_topology_from_goad_config")


def test_seed_topology_aws_has_no_bastion() -> None:
    cfg = _REPO / "ad" / "GOAD" / "data" / "config.json"
    topo = seed_topology(str(cfg), provider="aws")
    ids = {h["id"] for h in topo["hosts"]}
    assert "attackbox" in ids, "aws still has an attack box"
    assert "bastion" not in ids, "aws (SSM) should not add a bastion node"
    print("PASS test_seed_topology_aws_has_no_bastion")


def test_backup_yaml_versions() -> None:
    tmp = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
    tmp.write("a: 1\n")
    tmp.close()
    b1 = backup_yaml(tmp.name)
    b2 = backup_yaml(tmp.name)
    assert b1.endswith(".bak.1") and b2.endswith(".bak.2"), (b1, b2)
    assert os.path.isfile(b1) and os.path.isfile(b2), "backups not written"
    for p in (tmp.name, b1, b2):
        os.unlink(p)
    print("PASS test_backup_yaml_versions")


def test_merge_reseed_preserves_state_and_adds_nodes() -> None:
    existing = {
        "hosts": [
            {
                "id": "kingslanding",
                "role": "dc",
                "source": "config",
                "status": "running",
                "health": "healthy",
                "ip_private": "10.0.0.5",
                "cloud_id": "i-1",
                "last_checked_at": "T",
            },
            {"id": "winterfell", "role": "dc", "source": "config", "status": "running"},
        ],
        "edges": [],
        "layout": {
            "kingslanding": {"x": 100, "y": 50},
            "winterfell": {"x": 300, "y": 50},
        },
    }
    # Re-seed: kingslanding stays, winterfell gone, elk added.
    seeded = {
        "hosts": [
            {
                "id": "kingslanding",
                "role": "dc",
                "source": "config",
                "status": "unknown",
                "health": "unknown",
                "ip_private": None,
                "cloud_id": None,
                "last_checked_at": None,
            },
            {
                "id": "elk",
                "role": "linux",
                "source": "extension",
                "status": "unknown",
                "health": "unknown",
                "ip_private": None,
                "cloud_id": None,
                "last_checked_at": None,
            },
        ],
        "edges": [],
    }
    out = merge_reseed(existing, seeded)
    hosts = {h["id"]: h for h in out["hosts"]}
    assert set(hosts) == {"kingslanding", "elk"}, "node set should follow re-seed"
    # surviving host keeps live state
    assert hosts["kingslanding"]["status"] == "running", hosts["kingslanding"]
    assert hosts["kingslanding"]["ip_private"] == "10.0.0.5", hosts["kingslanding"]
    # new node has blank dynamic
    assert hosts["elk"]["status"] == "unknown", hosts["elk"]
    # layout kept only for survivors
    assert "kingslanding" in out["layout"] and "winterfell" not in out["layout"], out[
        "layout"
    ]
    print("PASS test_merge_reseed_preserves_state_and_adds_nodes")


if __name__ == "__main__":
    test_derive_snapshot_azure()
    test_derive_snapshot_aws_falls_back_to_source()
    test_derive_snapshot_unknown_env_raises()
    test_seed_topology_from_goad_config()
    test_seed_topology_aws_has_no_bastion()
    test_backup_yaml_versions()
    test_merge_reseed_preserves_state_and_adds_nodes()
    print("ALL PASS")
