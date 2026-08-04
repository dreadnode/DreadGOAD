"""Tests for the command registry, argv builder, and CLI runner (Phase 3).

Standalone:  python webapp/backend/tests/test_commands.py
"""

from __future__ import annotations

import asyncio
import pathlib
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend import commands  # noqa: E402
from webapp.backend.cli import run_command  # noqa: E402

_SESSION = {"anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"}}


def _argv(name: str, extra: list[str] | None = None) -> list[str]:
    return commands.build_argv(_SESSION, name, extra, repo_root="/repo")


def test_argv_injects_config_and_env() -> None:
    a = _argv("/up")
    assert a[0].endswith("dreadgoad"), a
    assert a[1:5] == ["--config", "/x/dreadgoad.yaml", "--env", "dev"], a
    assert a[5:] == ["up"], a
    print("PASS test_argv_injects_config_and_env")


def test_argv_multiword_and_flag_verbs() -> None:
    assert _argv("/reset")[5:] == ["lab", "reset"]
    assert _argv("/instances")[5:] == ["lab", "status", "--json"]
    assert _argv("/scrub")[5:] == ["score", "reset"]
    print("PASS test_argv_multiword_and_flag_verbs")


def test_argv_arg_shaped_commands() -> None:
    # /extensions: list vs provision
    assert _argv("/extensions")[5:] == ["extension", "list"]
    assert _argv("/extensions", ["elk"])[5:] == ["extension", "provision", "elk"]
    # /score: report path + passthrough flag
    assert _argv("/score", ["/tmp/r.jsonl", "--live-verify"])[5:] == [
        "score",
        "--report",
        "/tmp/r.jsonl",
        "--live-verify",
    ]
    # /variant: passthrough
    assert _argv("/variant", ["--name", "v2"])[5:] == [
        "variant",
        "generate",
        "--name",
        "v2",
    ]
    print("PASS test_argv_arg_shaped_commands")


def test_registry_flags_and_parsing() -> None:
    assert commands.REGISTRY["/up"].long_running is True
    assert commands.REGISTRY["/instances"].long_running is False
    assert commands.REGISTRY["/destroy"].verb == ("infra", "destroy")
    assert commands.is_command("/health") and not commands.is_command("hello there")
    assert commands.parse_command("/score /tmp/r.jsonl --live-verify") == (
        "/score",
        ["/tmp/r.jsonl", "--live-verify"],
    )
    try:
        _argv("/nope")
        raise AssertionError("expected KeyError for unknown command")
    except KeyError:
        pass
    print("PASS test_registry_flags_and_parsing")


async def test_runner_streams_and_returns_rc() -> None:
    """Runner streams lines and reports the exit code (stubbed CLI)."""
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "fakecli.sh"
        stub.write_text("#!/usr/bin/env bash\necho line-one\necho line-two\nexit 3\n")
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        seen: list[str] = []
        rc, out = await run_command([str(stub)], cwd=d, on_line=seen.append)
        assert rc == 3, f"rc={rc}"
        assert seen == ["line-one", "line-two"], seen
        assert "line-one" in out and "line-two" in out, out
        print("PASS test_runner_streams_and_returns_rc")


def main() -> None:
    test_argv_injects_config_and_env()
    test_argv_multiword_and_flag_verbs()
    test_argv_arg_shaped_commands()
    test_registry_flags_and_parsing()
    asyncio.run(test_runner_streams_and_returns_rc())
    print("ALL PASS")


if __name__ == "__main__":
    main()
