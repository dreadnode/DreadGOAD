"""Tests for the command registry, argv builder, and CLI runner (Phase 3).

Standalone:  python webapp/backend/tests/test_commands.py
"""

from __future__ import annotations

import asyncio
import pathlib
import shutil
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


def test_dispatch_and_agent_commands() -> None:
    # arg-flexible/mutating → agent; deterministic reads + /destroy → direct
    assert commands.REGISTRY["/up"].dispatch == "agent"
    assert commands.REGISTRY["/variant"].dispatch == "agent"
    assert commands.REGISTRY["/instances"].dispatch == "direct"
    assert commands.REGISTRY["/destroy"].dispatch == "direct", (
        "destroy is operator-only"
    )
    assert commands.AGENT_COMMANDS == frozenset(
        {"/up", "/provision", "/reset", "/variant", "/extensions", "/score"}
    ), commands.AGENT_COMMANDS
    print("PASS test_dispatch_and_agent_commands")


def test_expand_command_prompt() -> None:
    p = commands.expand_command_prompt("/up", ["using", "the", "variant"])
    assert "/up" in p and "run_dreadgoad" in p, p
    assert "using the variant" in p, p
    assert "raw cloud CLI" in p, "must forbid raw cloud CLI"
    # /up ships prompts/up.md → guidance section present.
    assert "## Command-specific guidance" in p, "up.md guidance expected"
    # no-args form is explicit
    assert "(no extra arguments given)" in commands.expand_command_prompt(
        "/provision", []
    )
    print("PASS test_expand_command_prompt")


def test_load_prompt_and_guidance_injection() -> None:
    """Per-command markdown is loaded and injected; missing files → None."""
    # loader: existing stem vs missing stem
    assert commands.load_prompt("system") is not None, "system.md must exist"
    assert commands.load_prompt("does-not-exist") is None
    # Every agent command ships a guidance file, injected before the request line.
    for name in sorted(commands.AGENT_COMMANDS):
        p = commands.expand_command_prompt(name, [])
        assert "## Command-specific guidance" in p, f"{name} missing guidance"
        gi, oi = p.index("## Command-specific guidance"), p.index("Operator's request:")
        assert gi < oi, f"{name}: guidance must precede operator request"
    # Real content from specific files (not hallucinated).
    assert "variant generate" in commands.expand_command_prompt("/variant", [])
    assert "report path" in commands.expand_command_prompt("/score", []).lower()
    assert "extension list" in commands.expand_command_prompt("/extensions", [])
    assert "pipeline" in commands.expand_command_prompt("/up", []).lower()
    print("PASS test_load_prompt_and_guidance_injection")


def test_guidance_fallback_when_file_absent() -> None:
    """With no prompts/<cmd>.md, the generic template stands (no guidance block)."""
    orig = commands.load_prompt
    commands.load_prompt = lambda stem: None  # type: ignore[assignment]
    try:
        p = commands.expand_command_prompt("/up", ["x"])
        assert "## Command-specific guidance" not in p, "no block when file absent"
        assert "run_dreadgoad" in p and "raw cloud CLI" in p, "generic template intact"
    finally:
        commands.load_prompt = orig
    print("PASS test_guidance_fallback_when_file_absent")


def test_resolve_bin_prefers_repo_binary() -> None:
    """The repo's freshly-built cli/dreadgoad wins over PATH (C3)."""
    with tempfile.TemporaryDirectory() as d:
        repo = pathlib.Path(d)
        cli = repo / "cli"
        cli.mkdir()
        binp = cli / "dreadgoad"
        binp.write_text("#!/usr/bin/env bash\n")
        binp.chmod(binp.stat().st_mode | stat.S_IEXEC)
        assert commands.resolve_bin(repo) == str(binp), "must prefer repo binary"

        # No repo binary → falls back to the expected repo path (clear error),
        # unless dreadgoad is on PATH (env-dependent, so only assert the miss case).
        empty = pathlib.Path(d) / "empty"
        empty.mkdir()
        got = commands.resolve_bin(empty)
        assert got in (
            shutil.which("dreadgoad"),
            str(empty / "cli" / "dreadgoad"),
        ), got
    print("PASS test_resolve_bin_prefers_repo_binary")


def test_parse_command_shlex() -> None:
    """Quoted args (paths with spaces) survive tokenization (C5)."""
    assert commands.parse_command('/score "/tmp/my report.jsonl" --live-verify') == (
        "/score",
        ["/tmp/my report.jsonl", "--live-verify"],
    )
    # Unbalanced quotes → graceful fallback to plain split (no crash).
    name, extra = commands.parse_command('/variant --name "v2')
    assert name == "/variant" and extra == ["--name", '"v2'], (name, extra)
    print("PASS test_parse_command_shlex")


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
    test_dispatch_and_agent_commands()
    test_expand_command_prompt()
    test_load_prompt_and_guidance_injection()
    test_guidance_fallback_when_file_absent()
    test_resolve_bin_prefers_repo_binary()
    test_parse_command_shlex()
    asyncio.run(test_runner_streams_and_returns_rc())
    print("ALL PASS")


if __name__ == "__main__":
    main()
