"""Tests for long-op streaming + cancellation (Phase 6: T6.1).

Standalone:  python console/backend/tests/test_longops.py
"""

from __future__ import annotations

import asyncio
import pathlib
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

import json  # noqa: E402

from console.backend.cli import capture, start_command  # noqa: E402


def _script(body: str) -> str:
    d = tempfile.mkdtemp()
    p = pathlib.Path(d) / "stub.sh"
    p.write_text("#!/usr/bin/env bash\n" + body)
    p.chmod(p.stat().st_mode | stat.S_IEXEC)
    return str(p)


async def test_stream_lines_live() -> None:
    stub = _script("echo a\necho b\necho c\nexit 0\n")
    rc = await start_command([stub], cwd=".")
    got = [line async for line in rc.stream_lines()]
    assert got == ["a", "b", "c"], got
    assert rc.returncode == 0, rc.returncode
    print("PASS test_stream_lines_live")


async def test_cancel_sigint_stops_before_completion() -> None:
    # Prints 'started', sleeps, then would print 'done'. SIGINT kills it first.
    stub = _script("echo started\nsleep 5\necho done\n")
    rc = await start_command([stub], cwd=".")
    it = rc.stream_lines()
    first = await it.__anext__()
    assert first == "started", first

    rc.cancel()  # SIGINT

    rest = []
    async for line in it:
        rest.append(line)
    assert "done" not in rest, (
        f"command should have been cancelled before 'done': {rest}"
    )
    assert rc.returncode != 0, "cancelled command should have non-zero exit"
    print("PASS test_cancel_sigint_stops_before_completion")


async def test_capture_separates_stdout_stderr() -> None:
    """JSON on stdout must survive stderr noise (the hook parses stdout)."""
    stub = _script('echo "[1, 2]"\necho "WARN: some log noise" 1>&2\nexit 0\n')
    rc, out, err = await capture([stub], cwd=".")
    assert rc == 0, rc
    assert out.strip() == "[1, 2]", f"stdout polluted: {out!r}"
    assert "WARN" in err, f"stderr not captured: {err!r}"
    assert json.loads(out) == [1, 2], "stdout must parse as clean JSON"
    print("PASS test_capture_separates_stdout_stderr")


async def _main() -> None:
    await test_stream_lines_live()
    await test_cancel_sigint_stops_before_completion()
    await test_capture_separates_stdout_stderr()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
