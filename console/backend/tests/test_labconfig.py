"""Tests for snapshot derivation, topology seeding, and yaml backup (Phase 2).

Standalone:  python console/backend/tests/test_labconfig.py
"""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.labconfig import (  # noqa: E402
    backup_yaml,
    create_config,
    derive_snapshot,
    list_environments,
    merge_reseed,
    seed_topology,
    write_new_env,
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


def test_env_setting_prefers_new_name_accepts_legacy() -> None:
    """DREADGOAD_CONSOLE_* wins; the pre-rename DREADGOAD_WEBAPP_* still works."""
    import os

    from console.backend import paths

    keys = ("DREADGOAD_CONSOLE_PORT", "DREADGOAD_WEBAPP_PORT")
    saved = {k: os.environ.pop(k, None) for k in keys}
    try:
        assert paths.setting("PORT") is None, "unset -> None"
        assert paths.setting("PORT", "7331") == "7331", "default honoured"

        os.environ["DREADGOAD_WEBAPP_PORT"] = "9001"
        assert paths.setting("PORT") == "9001", "legacy name still read"

        os.environ["DREADGOAD_CONSOLE_PORT"] = "9002"
        assert paths.setting("PORT") == "9002", "new name wins over legacy"

        os.environ["DREADGOAD_CONSOLE_PORT"] = ""
        assert paths.setting("PORT") == "9001", "blank new -> falls back to legacy"
    finally:
        for k, v in saved.items():
            os.environ.pop(k, None)
            if v is not None:
                os.environ[k] = v
    print("PASS test_env_setting_prefers_new_name_accepts_legacy")


def test_state_root_migrates_legacy_dir() -> None:
    """The pre-rename .dreadgoad/webapp/ is moved, not abandoned."""
    import os
    import tempfile

    from console.backend import paths

    saved = {
        k: os.environ.pop(k, None)
        for k in ("DREADGOAD_CONSOLE_STATE_ROOT", "DREADGOAD_WEBAPP_STATE_ROOT")
    }
    try:
        with tempfile.TemporaryDirectory() as d:
            repo = pathlib.Path(d)
            (repo / "ad").mkdir()  # makes repo_root() resolve here
            legacy = repo / ".dreadgoad" / "webapp"
            legacy.mkdir(parents=True)
            (legacy / "state.db").write_text("session data")

            orig = paths.repo_root
            paths.repo_root = lambda: repo  # type: ignore[assignment]
            try:
                root = paths.state_root()
                assert root == repo / ".dreadgoad" / "console", root
                assert (root / "state.db").read_text() == "session data", "data moved"
                assert not legacy.exists(), "legacy dir should be gone after the move"

                # Idempotent: a second call must not fail or move anything.
                assert paths.state_root() == root
            finally:
                paths.repo_root = orig  # type: ignore[assignment]
    finally:
        for k, v in saved.items():
            if v is not None:
                os.environ[k] = v
    print("PASS test_state_root_migrates_legacy_dir")


def test_derive_snapshot_prefers_env_region_over_file_region() -> None:
    """Mirrors Config.ResolveRegion: the env's own region beats the file's."""
    tmp = tempfile.NamedTemporaryFile("w", suffix=".yaml", delete=False)
    tmp.write(
        "provider: aws\n"
        "region: us-east-1\n"
        "environments:\n"
        "  near:\n"
        "    region: eu-west-2\n"
        "    vpc_cidr: 10.5.0.0/16\n"
        "  far:\n"
        "    vpc_cidr: 10.6.0.0/16\n"
    )
    tmp.close()
    assert derive_snapshot(tmp.name, "near")["region"] == "eu-west-2", (
        "env region must win — the CLI would use it, so the header must show it"
    )
    assert derive_snapshot(tmp.name, "far")["region"] == "us-east-1", (
        "file-level region is the fallback for envs that don't declare one"
    )
    os.unlink(tmp.name)
    print("PASS test_derive_snapshot_prefers_env_region_over_file_region")


def test_create_config_writes_a_usable_file() -> None:
    with tempfile.TemporaryDirectory() as d:
        # A nested dir that does not exist yet — create_config must make it.
        path = os.path.join(d, "configs", "azure-lab.yaml")
        create_config(
            path,
            "azure",
            "redteam",
            {"variant": True, "variant_target": "ad/GOAD-rt", "variant_name": "rt"},
            region="eastus",
        )
        # Round-trip through the reader the console actually uses.
        snap = derive_snapshot(path, "redteam")
        assert snap["provider"] == "azure", snap
        assert snap["region"] == "eastus", snap
        assert snap["lab"] == "ad/GOAD-rt", snap
        assert "azure" in snap, "provider block should be built for azure"

        import yaml as _yaml

        with open(path) as f:
            raw = _yaml.safe_load(f)
        assert raw["env"] == "redteam", (
            "a default env makes the file usable by a bare CLI run"
        )
        # 0o600, not config_cmd.go's 0o644 — this file grows secrets later.
        assert oct(os.stat(path).st_mode)[-3:] == "600", oct(os.stat(path).st_mode)
    print("PASS test_create_config_writes_a_usable_file")


def test_create_config_refuses_to_overwrite() -> None:
    """The failure that would silently adopt someone else's provider."""
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "dreadgoad.yaml")
        with open(path, "w") as f:
            f.write(
                "provider: aws\nenvironments:\n  prod:\n    vpc_cidr: 10.2.0.0/16\n"
            )
        try:
            create_config(path, "azure", "new", {})
            raise AssertionError("expected FileExistsError")
        except FileExistsError:
            pass
        # The original must be byte-for-byte untouched, not backed up and replaced.
        with open(path) as f:
            assert "provider: aws" in f.read(), "existing config was modified"
    print("PASS test_create_config_refuses_to_overwrite")


def test_create_config_rejects_unknown_provider_and_blank_env() -> None:
    with tempfile.TemporaryDirectory() as d:
        for provider, env, label in (
            ("gcp", "x", "unknown provider"),
            ("aws", "   ", "blank env name"),
        ):
            path = os.path.join(d, f"{label.replace(' ', '-')}.yaml")
            try:
                create_config(path, provider, env, {})
                raise AssertionError(f"expected ValueError for {label}")
            except ValueError:
                pass
            assert not os.path.exists(path), f"{label} must not leave a file behind"
    print("PASS test_create_config_rejects_unknown_provider_and_blank_env")


_FIXTURE_COMMENTED = """\
# DreadGOAD CLI Configuration
env: staging
provider: aws  # Provider: aws, azure, proxmox, ludus
# region: us-west-2  # Fallback when environments.<env>.region is unset

# Proxmox settings (used when provider: proxmox)
# proxmox:
#   api_url: "https://192.168.20.80:8006"
#   password: ""  # Or set DREADGOAD_PROXMOX_PASSWORD env var

environments:
  staging:
    variant: false
    vpc_cidr: "10.1.0.0/16"   # keep this quoted
"""


def test_write_new_env_preserves_comments_and_formatting() -> None:
    """The regression that deleted 44 documented lines from a tracked config."""
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "dreadgoad.yaml")
        with open(path, "w") as f:
            f.write(_FIXTURE_COMMENTED)

        write_new_env(path, "redteam", {"variant": True, "vpc_cidr": "10.7.0.0/16"})

        with open(path) as f:
            after = f.read()

        for comment in (
            "# DreadGOAD CLI Configuration",
            "# Provider: aws, azure, proxmox, ludus",
            "# region: us-west-2",
            "# Proxmox settings (used when provider: proxmox)",
            "#   api_url:",
            "# Or set DREADGOAD_PROXMOX_PASSWORD env var",
            "# keep this quoted",
        ):
            assert comment in after, f"lost comment {comment!r}\n---\n{after}"

        # Quoting style survives too, so the diff shows only the addition.
        assert '"10.1.0.0/16"' in after, f"re-quoted an untouched value\n{after}"
        # Key order is not reshuffled.
        assert after.index("env: staging") < after.index("environments:"), after
        # And the new environment is actually there and readable.
        assert derive_snapshot(path, "redteam")["vpc_cidr"] == "10.7.0.0/16"
        assert derive_snapshot(path, "staging")["vpc_cidr"] == "10.1.0.0/16"

        # No line count lost: the old dump shrank this file, the new one grows it.
        assert after.count("\n") > _FIXTURE_COMMENTED.count("\n"), (
            "the file should have grown by the new env, not shrunk"
        )
    print("PASS test_write_new_env_preserves_comments_and_formatting")


def test_write_new_env_into_a_config_with_no_environments() -> None:
    """`environments:` absent, or present and empty — both must take the write.

    The empty case is the one that bites: an empty mapping is falsy, so a
    ``data.get(...) or {}`` resolves to a detached dict and the new environment
    never reaches the file.
    """
    for body, label in (
        ("provider: aws\n", "key absent"),
        ("provider: aws\nenvironments:\n", "key present but null"),
        ("provider: aws\nenvironments: {}\n", "key present but empty"),
    ):
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "dreadgoad.yaml")
            with open(path, "w") as f:
                f.write(body)
            write_new_env(path, "first", {"vpc_cidr": "10.3.0.0/16"})
            snap = derive_snapshot(path, "first")
            assert snap["vpc_cidr"] == "10.3.0.0/16", (label, snap)
    print("PASS test_write_new_env_into_a_config_with_no_environments")


def test_write_new_env_sets_top_level_keys() -> None:
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "dreadgoad.yaml")
        with open(path, "w") as f:
            f.write("# header\nprovider: aws\nenvironments:\n  a:\n    vpc_cidr: x\n")
        write_new_env(path, "b", {"vpc_cidr": "y"}, top_level={"region": "eu-west-1"})
        snap = derive_snapshot(path, "b")
        assert snap["region"] == "eu-west-1", snap
        assert snap["provider"] == "aws", snap
        with open(path) as f:
            assert "# header" in f.read(), "top_level update must not drop comments"
    print("PASS test_write_new_env_sets_top_level_keys")


def test_write_new_env_raises_value_error_on_malformed_yaml() -> None:
    """ruamel's YAMLError is not pyyaml's, so it must not reach the route raw.

    The routes catch ValueError/yaml.YAMLError and turn them into a 400. A bare
    ruamel.yaml.YAMLError shares no ancestry with either, so switching this
    function to the round-trip loader silently converted a malformed config from
    a 400 into a 500.
    """
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "dreadgoad.yaml")
        body = 'provider: aws\nenvironments:\n  dev:\n    vpc_cidr: "10.0.0.0/16\n'
        with open(path, "w") as f:
            f.write(body)
        try:
            write_new_env(path, "new", {})
            raise AssertionError("expected ValueError for malformed YAML")
        except ValueError as exc:
            assert "not valid YAML" in str(exc), exc
        # A file that could not be parsed must be left exactly as it was, and
        # must not have been backed up — the backup only makes sense once the
        # write is going ahead.
        with open(path) as f:
            assert f.read() == body, "malformed config was modified"
        assert not os.path.exists(f"{path}.bak.1"), "backed up a file it never wrote"
    print("PASS test_write_new_env_raises_value_error_on_malformed_yaml")


def test_list_environments_reports_the_region_the_cli_would_use() -> None:
    """Per-env regions must reach the caller, not just the file-level key.

    The console warns "this config sets no region" from this value. Reporting
    only ``data["region"]`` made a config that declares regions per environment
    — legal, and what ResolveRegion prefers — look like it had none.
    """
    cases = {
        "provider: aws\nenvironments:\n  prod:\n    region: eu-west-2\n": "eu-west-2",
        "provider: aws\nregion: us-east-1\nenvironments:\n  prod: {}\n": "us-east-1",
        # Env beats file, matching config.go:478-484.
        "provider: aws\nregion: us-east-1\nenvironments:\n"
        "  prod:\n    region: eu-west-2\n": "eu-west-2",
        "provider: azure\nenvironments:\n  prod: {}\n": None,
        # Azure reads cfg.Region directly (infra_cmd.go:215) and never calls
        # ResolveRegion, so a per-environment region is ignored: this config
        # deploys nothing, and must not be reported as having a region.
        "provider: azure\nenvironments:\n  prod:\n    region: eastus\n": None,
        "provider: azure\nregion: centralus\nenvironments:\n"
        "  prod:\n    region: eastus\n": "centralus",
    }
    for body, expected in cases.items():
        with tempfile.TemporaryDirectory() as d:
            path = os.path.join(d, "dreadgoad.yaml")
            with open(path, "w") as f:
                f.write(body)
            got = list_environments(path)["env_regions"]["prod"]
            assert got == expected, (body, got, expected)
            # The snapshot must agree. Dropping this assertion when the Azure
            # cases were added is exactly how the two drifted apart: the header
            # showed a region, the scaffold used it, and the warning said there
            # was none — all from the same file.
            assert got == derive_snapshot(path, "prod")["region"], body
    print("PASS test_list_environments_reports_the_region_the_cli_would_use")


def test_environments_must_be_a_mapping() -> None:
    """A '-' list where a mapping belongs is a 400, not an AttributeError 500."""
    with tempfile.TemporaryDirectory() as d:
        path = os.path.join(d, "dreadgoad.yaml")
        with open(path, "w") as f:
            f.write("provider: aws\nenvironments:\n  - staging\n  - prod\n")
        for call in (
            lambda: list_environments(path),
            lambda: derive_snapshot(path, "staging"),
            lambda: write_new_env(path, "new", {}),
        ):
            try:
                call()
                raise AssertionError("expected ValueError for a list-shaped envs key")
            except ValueError as exc:
                assert "must be a mapping" in str(exc), exc
    print("PASS test_environments_must_be_a_mapping")


if __name__ == "__main__":
    test_derive_snapshot_azure()
    test_derive_snapshot_aws_falls_back_to_source()
    test_derive_snapshot_unknown_env_raises()
    test_seed_topology_from_goad_config()
    test_seed_topology_aws_has_no_bastion()
    test_backup_yaml_versions()
    test_merge_reseed_preserves_state_and_adds_nodes()
    test_env_setting_prefers_new_name_accepts_legacy()
    test_state_root_migrates_legacy_dir()
    test_derive_snapshot_prefers_env_region_over_file_region()
    test_create_config_writes_a_usable_file()
    test_create_config_refuses_to_overwrite()
    test_create_config_rejects_unknown_provider_and_blank_env()
    test_write_new_env_preserves_comments_and_formatting()
    test_write_new_env_into_a_config_with_no_environments()
    test_write_new_env_sets_top_level_keys()
    test_write_new_env_raises_value_error_on_malformed_yaml()
    test_list_environments_reports_the_region_the_cli_would_use()
    test_environments_must_be_a_mapping()
    print("ALL PASS")
