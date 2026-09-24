"""Tests for post-destroy removal of console-managed environment artifacts."""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile
import typing as t

import yaml

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import environment_purge, paths, scaffold  # noqa: E402


def _session(config: pathlib.Path) -> dict[str, t.Any]:
    return {
        "anchor": {"config_path": str(config), "env": "ahab"},
        "snapshot": {
            "provider": "azure",
            "deployment": "goad-deployment",
            "lab": "GOAD-ahab",
        },
    }


def test_variant_purge_removes_only_owned_artifacts() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        state = root / ".dreadgoad" / "console"
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(state)
        (root / "ansible").mkdir()

        config = paths.configs_root() / "ahab.yaml"
        config.write_text(
            yaml.safe_dump(
                {
                    "env": "ahab",
                    "environments": {
                        "ahab": {
                            "provider": "azure",
                            "region": "centralus",
                            "variant": True,
                            "variant_source": "ad/GOAD",
                            "variant_target": "ad/GOAD-ahab",
                        }
                    },
                }
            )
        )
        infra = pathlib.Path(
            scaffold.infra_env_dir(str(root), "azure", "ahab", "goad-deployment")
        )
        inventory = root / "ahab-inventory"
        variant = root / "ad" / "GOAD-ahab"
        unrelated = root / "ad" / "GOAD"
        for target in (infra, variant, unrelated):
            target.mkdir(parents=True)
            (target / "keep.txt").write_text(target.name)
        inventory.write_text("[all]\n")
        marker = scaffold.record_ownership(
            config, "ahab", root, "azure", "goad-deployment"
        )
        assert marker is not None

        plan = environment_purge.build_plan(_session(config))
        removed = environment_purge.execute(plan)

        expected = {
            str(path.resolve()) for path in (infra, inventory, variant, marker, config)
        }
        assert set(removed) == expected, (removed, expected)
        assert not infra.exists() and not inventory.exists() and not variant.exists()
        assert not config.exists()
        assert unrelated.is_dir(), "purge crossed into the authored base range"
        assert not list(root.glob(".dreadgoad-purge-*")), "staging directory leaked"
    print("PASS test_variant_purge_removes_only_owned_artifacts")


def test_base_range_purge_removes_generated_data() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory).resolve()
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(
            root / ".dreadgoad" / "console"
        )
        (root / "ansible").mkdir()

        config = paths.configs_root() / "ahab.yaml"
        config.write_text(
            yaml.safe_dump(
                {"environments": {"ahab": {"provider": "azure", "region": "centralus"}}}
            )
        )
        data_dir = root / "ad" / "GOAD" / "data"
        data_dir.mkdir(parents=True)
        overlay = data_dir / "ahab-overlay.json"
        generated_config = data_dir / "ahab-config.json"
        authored = data_dir / "config.json"
        overlay.write_text("{}")
        generated_config.write_text("{}")
        authored.write_text("{}")
        marker = scaffold.record_ownership(
            config, "ahab", root, "azure", "goad-deployment"
        )
        assert marker is not None

        session = _session(config)
        session["snapshot"]["lab"] = "ad/GOAD"
        plan = environment_purge.build_plan(session)
        assert overlay in plan.targets and generated_config in plan.targets
        environment_purge.execute(plan)

        assert not overlay.exists() and not generated_config.exists()
        assert authored.exists(), "purge removed an authored base-range file"
    print("PASS test_base_range_purge_removes_generated_data")


def test_purge_rejects_imported_or_shared_configs() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(
            root / ".dreadgoad" / "console"
        )
        (root / "ansible").mkdir()
        document = {
            "environments": {"ahab": {"provider": "azure", "region": "centralus"}}
        }

        imported = root / "dreadgoad.yaml"
        imported.write_text(yaml.safe_dump(document))
        try:
            environment_purge.build_plan(_session(imported))
        except ValueError as exc:
            assert "console-managed" in str(exc)
        else:
            raise AssertionError("imported config was accepted for purge")

        managed = paths.configs_root() / "shared.yaml"
        document["environments"]["other"] = {"provider": "azure"}
        managed.write_text(yaml.safe_dump(document))
        try:
            environment_purge.build_plan(_session(managed))
        except ValueError as exc:
            assert "only this environment" in str(exc)
        else:
            raise AssertionError("shared config was accepted for purge")
    print("PASS test_purge_rejects_imported_or_shared_configs")


def test_purge_requires_scaffold_ownership_proof() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(
            root / ".dreadgoad" / "console"
        )
        (root / "ansible").mkdir()
        config = paths.configs_root() / "ahab.yaml"
        config.write_text(
            yaml.safe_dump(
                {"environments": {"ahab": {"provider": "azure", "region": "centralus"}}}
            )
        )
        infra = pathlib.Path(
            scaffold.infra_env_dir(str(root), "azure", "ahab", "goad-deployment")
        )
        infra.mkdir(parents=True)
        (root / "ahab-inventory").write_text("[all]\n")

        try:
            environment_purge.build_plan(_session(config))
        except ValueError as exc:
            assert "ownership marker" in str(exc)
        else:
            raise AssertionError("unowned project artifacts were accepted for purge")
        assert infra.exists(), "ownership validation mutated project artifacts"
    print("PASS test_purge_requires_scaffold_ownership_proof")


def test_purge_rejects_tampered_owned_paths() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(
            root / ".dreadgoad" / "console"
        )
        (root / "ansible").mkdir()
        config = paths.configs_root() / "ahab.yaml"
        document = {
            "environments": {
                "ahab": {
                    "provider": "azure",
                    "variant": True,
                    "variant_source": "ad/GOAD",
                    "variant_target": "console",
                }
            }
        }
        config.write_text(yaml.safe_dump(document))

        try:
            environment_purge.build_plan(_session(config))
        except ValueError as exc:
            assert "not the generated target" in str(exc)
        else:
            raise AssertionError("tampered variant target was accepted for purge")

        document["environments"]["ahab"]["variant_target"] = "ad/GOAD-ahab"
        config.write_text(yaml.safe_dump(document))
        session = _session(config)
        session["snapshot"]["deployment"] = "../.."
        try:
            environment_purge.build_plan(session)
        except ValueError as exc:
            assert "unsafe deployment name" in str(exc)
        else:
            raise AssertionError("path-traversing deployment was accepted for purge")
    print("PASS test_purge_rejects_tampered_owned_paths")


def test_purge_rejects_symlinked_artifacts() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(
            root / ".dreadgoad" / "console"
        )
        (root / "ansible").mkdir()
        config = paths.configs_root() / "ahab.yaml"
        config.write_text(
            yaml.safe_dump(
                {"environments": {"ahab": {"provider": "azure", "region": "centralus"}}}
            )
        )
        marker = scaffold.record_ownership(
            config, "ahab", root, "azure", "goad-deployment"
        )
        assert marker is not None
        authored = root / "ad" / "GOAD" / "data"
        authored.mkdir(parents=True)
        (authored / "keep.txt").write_text("authored")
        inventory = root / "ahab-inventory"
        inventory.symlink_to(authored, target_is_directory=True)

        try:
            environment_purge.build_plan(_session(config))
        except ValueError as exc:
            assert "symlinked path" in str(exc)
        else:
            raise AssertionError("symlinked inventory was accepted for purge")
        assert authored.is_dir() and (authored / "keep.txt").exists()
        assert inventory.is_symlink()
    print("PASS test_purge_rejects_symlinked_artifacts")


def test_purge_rechecks_symlinks_before_move() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory).resolve()
        artifact = root / "range-inventory"
        authored = root / "authored"
        artifact.write_text("owned")
        authored.mkdir()
        plan = environment_purge.PurgePlan(
            "range", root, root / "range.yaml", (artifact,)
        )

        artifact.unlink()
        artifact.symlink_to(authored, target_is_directory=True)
        try:
            environment_purge.execute(plan)
        except RuntimeError as exc:
            assert "symlinked path" in str(exc)
        else:
            raise AssertionError("post-plan symlink replacement was purged")
        assert artifact.is_symlink() and authored.is_dir()
        assert not list(root.glob(".dreadgoad-purge-*"))
    print("PASS test_purge_rechecks_symlinks_before_move")


def test_purge_rolls_back_when_a_move_fails() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory).resolve()
        first = root / "first"
        second = root / "second"
        first.write_text("one")
        second.write_text("two")
        plan = environment_purge.PurgePlan(
            "range", root, root / "range.yaml", (first, second)
        )
        original_replace = environment_purge.os.replace

        def fail_second(source, destination):  # noqa: ANN001, ANN202
            if pathlib.Path(source) == second:
                raise OSError("injected move failure")
            return original_replace(source, destination)

        environment_purge.os.replace = fail_second
        try:
            try:
                environment_purge.execute(plan)
            except OSError as exc:
                assert "injected move failure" in str(exc)
            else:
                raise AssertionError("injected purge failure did not propagate")
        finally:
            environment_purge.os.replace = original_replace

        assert first.read_text() == "one", "first artifact was not restored"
        assert second.read_text() == "two", "unmoved artifact was damaged"
        assert not list(root.glob(".dreadgoad-purge-*")), "rollback staging leaked"
    print("PASS test_purge_rolls_back_when_a_move_fails")


def main() -> None:
    original = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    try:
        test_variant_purge_removes_only_owned_artifacts()
        test_base_range_purge_removes_generated_data()
        test_purge_rejects_imported_or_shared_configs()
        test_purge_requires_scaffold_ownership_proof()
        test_purge_rejects_tampered_owned_paths()
        test_purge_rejects_symlinked_artifacts()
        test_purge_rechecks_symlinks_before_move()
        test_purge_rolls_back_when_a_move_fails()
    finally:
        if original is None:
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
        else:
            os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = original
    print("ALL PASS")


if __name__ == "__main__":
    main()
