"""Tests for infrastructure scaffolding via `dreadgoad env create`.

Standalone:  python console/backend/tests/test_scaffold.py
"""

from __future__ import annotations

import asyncio
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import scaffold  # noqa: E402


def _capture(rc: int = 0, out: str = "created", err: str = ""):
    seen: list[tuple[list[str], str]] = []

    async def run(argv: list[str], cwd: str) -> tuple[int, str, str]:
        seen.append((argv, cwd))
        return rc, out, err

    return run, seen


def _project(root: pathlib.Path) -> pathlib.Path:
    """A tree the CLI's project-root walk will accept."""
    (root / "ansible").mkdir(parents=True, exist_ok=True)
    (root / "dreadgoad.yaml").write_text("provider: azure\nregion: centralus\n")
    return root / "dreadgoad.yaml"


def test_infra_env_dir_matches_the_cli_layout() -> None:
    # config.go:504-511 — azure nests under infra/azure/<deployment>.
    assert scaffold.infra_env_dir("/r", "azure", "rt").endswith(
        "/infra/azure/goad-deployment/rt"
    )
    assert scaffold.infra_env_dir("/r", "aws", "rt").endswith(
        "/infra/goad-deployment/rt"
    )
    print("PASS test_infra_env_dir_matches_the_cli_layout")


def test_preflight_passes_on_a_clean_tree() -> None:
    with tempfile.TemporaryDirectory() as d:
        assert scaffold.preflight(d, "azure", "rt", "ad/GOAD-rt") == []
    print("PASS test_preflight_passes_on_a_clean_tree")


def test_preflight_blocks_the_unrecoverable_states() -> None:
    """Both halves of the state `env create --force` cannot recover from."""
    # Existing infra dir.
    with tempfile.TemporaryDirectory() as d:
        pathlib.Path(scaffold.infra_env_dir(d, "azure", "rt")).mkdir(parents=True)
        problems = scaffold.preflight(d, "azure", "rt", "ad/GOAD-rt")
        assert len(problems) == 1, problems
        assert "already has infrastructure" in problems[0], problems

    # Existing variant target — the half --force does NOT skip.
    with tempfile.TemporaryDirectory() as d:
        (pathlib.Path(d) / "ad" / "GOAD-rt").mkdir(parents=True)
        problems = scaffold.preflight(d, "azure", "rt", "ad/GOAD-rt")
        assert len(problems) == 1, problems
        assert "variant generator refuses" in problems[0], problems

    # Both — the operator must be told about both, not just the first.
    with tempfile.TemporaryDirectory() as d:
        pathlib.Path(scaffold.infra_env_dir(d, "azure", "rt")).mkdir(parents=True)
        (pathlib.Path(d) / "ad" / "GOAD-rt").mkdir(parents=True)
        assert len(scaffold.preflight(d, "azure", "rt", "ad/GOAD-rt")) == 2
    print("PASS test_preflight_blocks_the_unrecoverable_states")


def test_preflight_ignores_the_variant_target_when_not_a_variant() -> None:
    with tempfile.TemporaryDirectory() as d:
        (pathlib.Path(d) / "ad" / "GOAD-rt").mkdir(parents=True)
        assert scaffold.preflight(d, "azure", "rt", None) == []
    print("PASS test_preflight_ignores_the_variant_target_when_not_a_variant")


def test_build_argv_shape() -> None:
    argv = scaffold.build_argv(
        "/c.yaml",
        "rt",
        "centralus",
        variant=True,
        variant_source="ad/SCCM",
        vpc_cidr="10.55.0.0/16",
    )
    assert argv[1:] == [
        "--config",
        "/c.yaml",
        "--env",
        "rt",
        "env",
        "create",
        "rt",
        "--region",
        "centralus",
        "--vpc-cidr",
        "10.55.0.0/16",
        "--variant",
        "--variant-source",
        "ad/SCCM",
    ], argv

    # Without a variant, neither variant flag appears.
    plain = scaffold.build_argv("/c.yaml", "rt", "centralus")
    assert "--variant" not in plain and "--variant-source" not in plain, plain
    assert "--vpc-cidr" not in plain, plain
    print("PASS test_build_argv_shape")


async def test_scaffold_runs_in_the_config_tree() -> None:
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d) / "elsewhere"
        cfg = _project(root)
        run, seen = _capture()
        ok, _out = await scaffold.scaffold_env(
            str(cfg), "rt", "centralus", provider="azure", capture_command=run
        )
        assert ok, "clean tree should scaffold"
        _argv, cwd = seen[0]
        assert cwd == str(root.resolve()), (
            f"must run in the config's own tree, not the console's: {cwd}"
        )
    print("PASS test_scaffold_runs_in_the_config_tree")


async def test_scaffold_refuses_before_spawning_when_preflight_fails() -> None:
    """Nothing is run when the state is one env create cannot recover from."""
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d) / "elsewhere"
        cfg = _project(root)
        pathlib.Path(scaffold.infra_env_dir(str(root), "azure", "rt")).mkdir(
            parents=True
        )
        run, seen = _capture()
        ok, out = await scaffold.scaffold_env(
            str(cfg), "rt", "centralus", provider="azure", capture_command=run
        )
        assert not ok, out
        assert seen == [], "must not spawn env create when preflight failed"
        assert "already has infrastructure" in out, out
    print("PASS test_scaffold_refuses_before_spawning_when_preflight_fails")


async def test_scaffold_refuses_an_empty_region() -> None:
    """`--region ""` would defer to the CLI's own resolution and fail obscurely."""
    with tempfile.TemporaryDirectory() as d:
        cfg = _project(pathlib.Path(d) / "elsewhere")
        run, seen = _capture()
        for region in ("", "   "):
            ok, out = await scaffold.scaffold_env(
                str(cfg), "rt", region, provider="azure", capture_command=run
            )
            assert not ok, (region, out)
            assert "requires one" in out, out
        assert seen == [], "must not spawn env create without a region"
    print("PASS test_scaffold_refuses_an_empty_region")


async def test_scaffold_passes_argv_without_a_shell() -> None:
    """Env names reach argv verbatim; nothing is interpolated into a shell."""
    with tempfile.TemporaryDirectory() as d:
        cfg = _project(pathlib.Path(d) / "elsewhere")
        run, seen = _capture()
        hostile = "rt; rm -rf /"
        ok, _out = await scaffold.scaffold_env(
            str(cfg), hostile, "centralus", provider="azure", capture_command=run
        )
        assert ok
        argv, _cwd = seen[0]
        assert hostile in argv, argv
        assert not any(tok in ("sh", "-c", "bash") for tok in argv), argv
    print("PASS test_scaffold_passes_argv_without_a_shell")


async def test_scaffold_reports_a_failed_run() -> None:
    with tempfile.TemporaryDirectory() as d:
        cfg = _project(pathlib.Path(d) / "elsewhere")
        run, _ = _capture(rc=1, out="", err="reference environment not found")
        ok, out = await scaffold.scaffold_env(
            str(cfg), "rt", "centralus", provider="azure", capture_command=run
        )
        assert not ok and "reference environment not found" in out, out
    print("PASS test_scaffold_reports_a_failed_run")


async def test_scaffold_survives_a_spawn_that_raises() -> None:
    """A missing binary raises rather than returning non-zero."""
    with tempfile.TemporaryDirectory() as d:
        cfg = _project(pathlib.Path(d) / "elsewhere")

        async def boom(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
            raise FileNotFoundError(2, "No such file or directory")

        ok, out = await scaffold.scaffold_env(
            str(cfg), "rt", "centralus", provider="azure", capture_command=boom
        )
        assert not ok, out
        assert "could not run" in out, out
    print("PASS test_scaffold_survives_a_spawn_that_raises")


async def _main() -> None:
    test_infra_env_dir_matches_the_cli_layout()
    test_preflight_passes_on_a_clean_tree()
    test_preflight_blocks_the_unrecoverable_states()
    test_preflight_ignores_the_variant_target_when_not_a_variant()
    test_build_argv_shape()
    await test_scaffold_runs_in_the_config_tree()
    await test_scaffold_refuses_before_spawning_when_preflight_fails()
    await test_scaffold_refuses_an_empty_region()
    await test_scaffold_passes_argv_without_a_shell()
    await test_scaffold_reports_a_failed_run()
    await test_scaffold_survives_a_spawn_that_raises()
    print("ALL PASS")


if __name__ == "__main__":
    os.environ.setdefault("DREADGOAD_CONSOLE_STATE_ROOT", tempfile.mkdtemp())
    asyncio.run(_main())
