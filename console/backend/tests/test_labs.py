"""Tests for base-lab discovery behind `dreadgoad lab list --json`.

Standalone:  python console/backend/tests/test_labs.py
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import labs, paths  # noqa: E402


def _capture(payload: str, rc: int = 0) -> tuple[object, list[tuple[list[str], str]]]:
    """A stand-in for cli.capture that records how it was invoked."""
    seen: list[tuple[list[str], str]] = []

    async def run(argv: list[str], cwd: str) -> tuple[int, str, str]:
        seen.append((argv, cwd))
        return rc, payload, ""

    return run, seen


async def test_discover_labs_shapes_the_cli_output() -> None:
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d) / "ad" / "GOAD"
        variant = pathlib.Path(d) / "ad" / "GOAD-redteam"
        base.mkdir(parents=True)
        variant.mkdir(parents=True)
        # Only the generated one carries the generator's marker file.
        (variant / "mapping.json").write_text("{}")

        payload = json.dumps(
            [
                {
                    "name": "GOAD-redteam",
                    "path": str(variant),
                    "providers": ["aws"],
                    "hosts": ["dc01"],
                },
                {
                    "name": "GOAD",
                    "path": str(base),
                    "providers": ["aws", "azure"],
                    "hosts": ["dc01", "dc02", "srv02"],
                },
            ]
        )
        run, seen = _capture(payload)
        found = await labs.discover_labs(capture_command=run)  # type: ignore[arg-type]

        # Base labs first, generated variants after — regardless of CLI order.
        assert [x["name"] for x in found] == ["GOAD", "GOAD-redteam"], found
        assert found[0]["generated"] is False, found[0]
        assert found[1]["generated"] is True, (
            "a lab with mapping.json is a generated variant, whatever it is named"
        )
        # `dir` is what goes into variant_source: repo-relative, not the
        # absolute path the CLI reports.
        assert found[0]["dir"] == "ad/GOAD", found[0]
        assert found[0]["hosts"] == ["dc01", "dc02", "srv02"], found[0]
        assert found[0]["providers"] == ["aws", "azure"], found[0]

        # No config → runs at the repo root, because `lab list` treats its cwd
        # as the project root and needs one with an ad/ in it.
        argv, cwd = seen[0]
        assert argv[-3:] == ["lab", "list", "--json"], argv
        assert "--config" not in argv, argv
        assert cwd == str(paths.repo_root()), cwd
    print("PASS test_discover_labs_shapes_the_cli_output")


async def test_discover_labs_scopes_to_a_config_tree() -> None:
    """A config in another checkout must not list this repo's labs."""
    with tempfile.TemporaryDirectory() as d:
        other = pathlib.Path(d) / "elsewhere"
        (other / "ansible").mkdir(parents=True)  # the project-root marker
        cfg = other / "dreadgoad.yaml"
        cfg.write_text("provider: aws\n")

        run, seen = _capture("[]")
        await labs.discover_labs(str(cfg), capture_command=run)  # type: ignore[arg-type]
        argv, cwd = seen[0]
        assert "--config" in argv and str(cfg) in argv, argv
        assert cwd == str(other.resolve()), (
            f"must run in the config's own tree, not the console's: {cwd}"
        )
    print("PASS test_discover_labs_scopes_to_a_config_tree")


async def test_discover_labs_degrades_to_empty_rather_than_raising() -> None:
    """The modal must still open when the binary is missing or output is junk."""
    for payload, rc, label in (
        # Deliberately VALID json on a non-zero exit. With an empty payload this
        # passed even with the return-code guard removed, because json.loads("")
        # raised and the parse guard swallowed it — the exit code itself was
        # never actually pinned down.
        (
            '[{"name": "GOAD", "path": "/x", "providers": [], "hosts": []}]',
            1,
            "non-zero exit with parseable output",
        ),
        ("", 1, "non-zero exit, no output"),
        ("not json at all", 0, "unparsable stdout"),
        ('{"not": "a list"}', 0, "wrong JSON shape"),
        ("[{}]", 0, "entry with no name"),
        ('[{"name": ""}]', 0, "entry with a blank name"),
    ):
        run, _ = _capture(payload, rc)
        got = await labs.discover_labs(capture_command=run)  # type: ignore[arg-type]
        assert got == [], f"{label} should yield [], got {got}"
    print("PASS test_discover_labs_degrades_to_empty_rather_than_raising")


async def test_discover_labs_survives_a_spawn_that_raises() -> None:
    """A missing binary RAISES from create_subprocess_exec — it never returns.

    The stubbed runners above can only simulate a non-zero exit, which is a
    different code path: `resolve_bin` falls back to an expected path when
    nothing is built, so ENOENT is the likeliest failure of all, and it was
    reaching the route as a 500 while the docstring promised a graceful [].
    """
    for exc, label in (
        (FileNotFoundError(2, "No such file or directory"), "binary missing"),
        (PermissionError(13, "Permission denied"), "binary not executable"),
        (IsADirectoryError(21, "Is a directory"), "path is a directory"),
        (OSError(8, "Exec format error"), "not an executable format"),
    ):

        async def run(
            _argv: list[str], _cwd: str, _exc: BaseException = exc
        ) -> tuple[int, str, str]:
            raise _exc

        got = await labs.discover_labs(capture_command=run)  # type: ignore[arg-type]
        assert got == [], f"{label} should yield [], got {got}"
    print("PASS test_discover_labs_survives_a_spawn_that_raises")


async def test_discover_labs_survives_a_missing_path_field() -> None:
    """`generated` needs a path; without one the lab is still listed."""
    run, _ = _capture(json.dumps([{"name": "GOAD", "providers": [], "hosts": []}]))
    found = await labs.discover_labs(capture_command=run)  # type: ignore[arg-type]
    assert len(found) == 1, found
    assert found[0]["generated"] is False, found[0]
    assert found[0]["dir"] == "ad/GOAD", found[0]
    print("PASS test_discover_labs_survives_a_missing_path_field")


async def _main() -> None:
    await test_discover_labs_shapes_the_cli_output()
    await test_discover_labs_scopes_to_a_config_tree()
    await test_discover_labs_degrades_to_empty_rather_than_raising()
    await test_discover_labs_survives_a_spawn_that_raises()
    await test_discover_labs_survives_a_missing_path_field()
    print("ALL PASS")


if __name__ == "__main__":
    os.environ.setdefault("DREADGOAD_CONSOLE_STATE_ROOT", tempfile.mkdtemp())
    asyncio.run(_main())
