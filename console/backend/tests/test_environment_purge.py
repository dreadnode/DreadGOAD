"""Tests for post-destroy removal of console-managed environment artifacts."""

from __future__ import annotations

import os
import pathlib
import sys
import tempfile

import yaml

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import environment_purge, paths, scaffold  # noqa: E402


def _session(config: pathlib.Path) -> dict[str, object]:
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

        plan = environment_purge.build_plan(_session(config))
        removed = environment_purge.execute(plan)

        expected = {str(path.resolve()) for path in (infra, inventory, variant, config)}
        assert set(removed) == expected, (removed, expected)
        assert not infra.exists() and not inventory.exists() and not variant.exists()
        assert not config.exists()
        assert unrelated.is_dir(), "purge crossed into the authored base range"
        assert not list(root.glob(".dreadgoad-purge-*")), "staging directory leaked"
    print("PASS test_variant_purge_removes_only_owned_artifacts")


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


def test_purge_rolls_back_when_a_move_fails() -> None:
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory).resolve()
        first = root / "first"
        second = root / "second"
        first.write_text("one")
        second.write_text("two")
        plan = environment_purge.PurgePlan("range", root, (first, second))
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
        test_purge_rejects_imported_or_shared_configs()
        test_purge_rejects_tampered_owned_paths()
        test_purge_rolls_back_when_a_move_fails()
    finally:
        if original is None:
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
        else:
            os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = original
    print("ALL PASS")


if __name__ == "__main__":
    main()
