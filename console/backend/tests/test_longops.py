"""Tests for long-op streaming + cancellation (Phase 6: T6.1).

Standalone:  python console/backend/tests/test_longops.py
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import pathlib
import shlex
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

import json  # noqa: E402

from console.backend import cli  # noqa: E402
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


async def test_stream_lines_accepts_line_over_default_limit() -> None:
    """A single-line --json report past asyncio's 64 KiB default must stream whole."""
    stub = _script("head -c 204800 /dev/zero | tr '\\0' x\necho\necho after\nexit 0\n")
    rc = await start_command([stub], cwd=".")
    got = [line async for line in rc.stream_lines()]
    assert len(got) == 2, [len(line) for line in got]
    assert got[0] == "x" * 204800, len(got[0])
    assert got[1] == "after", got[1]
    assert rc.returncode == 0, rc.returncode
    print("PASS test_stream_lines_accepts_line_over_default_limit")


async def test_line_past_cap_is_yielded_in_bounded_pieces() -> None:
    """A line beyond the cap streams in pieces instead of buffering unbounded."""
    original = cli._MAX_LINE_BYTES
    cli._MAX_LINE_BYTES = 100_000
    try:
        report = json.dumps({"hosts": "y" * 250_000})
        code = f"import sys\nsys.stdout.write({report!r} + '\\nafter\\n')\n"
        rc = await start_command([sys.executable, "-c", code], cwd=".")
        got = [line async for line in rc.stream_lines()]
    finally:
        cli._MAX_LINE_BYTES = original
    assert got[-1] == "after", got[-1]
    pieces = got[:-1]
    assert len(pieces) > 1, [len(p) for p in pieces]
    assert "".join(pieces) == report, [len(p) for p in pieces]
    # The captured output keeps the real line boundaries, so the report parses.
    first, last = rc.output.split("\n")
    assert json.loads(first) == {"hosts": "y" * 250_000}
    assert last == "after", last
    # Each piece stops at the first read that crosses the cap.
    assert all(len(p) < 100_000 + 256 * 1024 for p in pieces), [len(p) for p in pieces]
    print("PASS test_line_past_cap_is_yielded_in_bounded_pieces")


async def test_slow_reader_still_gets_final_line_after_fast_burst() -> None:
    """A fast writer must not outrun a slow reader past the post-exit drain.

    ``stream_lines`` stops reading ``_DRAIN_BUDGET`` seconds after exit. Pipe
    backpressure keeps the writer blocked until the reader is nearly caught up,
    so only a small tail is left at exit. Without it, the whole burst is
    buffered in memory, the writer exits at once, and the final line — where
    the ``--json`` reports go — is dropped.
    """
    # 2 MiB burst in 20k lines: enough to outlast the drain budget if it is all
    # buffered up front, few enough lines to keep the test quick.
    lines = 20_000
    code = (
        "import sys\n"
        f"sys.stdout.write(('x' * 99 + '\\n') * {lines})\n"
        "sys.stdout.write('FINAL\\n')\n"
    )
    rc = await start_command([sys.executable, "-c", code], cwd=".")
    seen = 0
    last = ""
    async for line in rc.stream_lines():
        seen += 1
        last = line
        if seen % 1000 == 0:
            await asyncio.sleep(0.2)  # ~4 s total: well past the drain budget
    assert last == "FINAL", (seen, last)
    assert seen == lines + 1, seen
    print("PASS test_slow_reader_still_gets_final_line_after_fast_burst")


async def test_cancel_sigint_stops_before_completion() -> None:
    # Prints 'started', sleeps, then would print 'done'. SIGINT kills it first.
    stub = _script("echo started\nsleep 5\necho done\n")
    rc = await start_command([stub], cwd=".")
    it = rc.stream_lines()
    first = await it.__anext__()
    assert first == "started", first

    rc.cancel()  # SIGINT
    escalation = rc._kill_task
    rc.cancel()  # repeated Esc must reuse, not leak another timer
    assert rc._kill_task is escalation

    rest = []
    async for line in it:
        rest.append(line)
    assert "done" not in rest, (
        f"command should have been cancelled before 'done': {rest}"
    )
    assert rc.returncode != 0, "cancelled command should have non-zero exit"
    assert rc._kill_task is None, "completed command retained a delayed kill timer"
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


async def test_cancelled_capture_kills_its_process_group() -> None:
    """Cancelling capture cannot detach the child it was awaiting."""
    pidfile = pathlib.Path(tempfile.mkdtemp()) / "capture.pid"
    stub = _script(f"echo $$ > {shlex.quote(str(pidfile))}\nsleep 30\n")
    task = asyncio.create_task(capture([stub], cwd="."))

    pid: int | None = None
    for _ in range(50):
        if pidfile.exists():
            pid = int(pidfile.read_text().strip())
            break
        await asyncio.sleep(0.01)
    assert pid is not None, "capture child did not start"

    task.cancel()
    with contextlib.suppress(asyncio.CancelledError):
        await task

    for _ in range(50):
        try:
            os.kill(pid, 0)
        except ProcessLookupError:
            break
        await asyncio.sleep(0.01)
    else:
        raise AssertionError(f"cancelled capture left process {pid} alive")
    print("PASS test_cancelled_capture_kills_its_process_group")


async def test_capture_surviving_child_does_not_hold_pipes_open() -> None:
    """A helper inheriting separate pipes cannot wedge capture after CLI exit."""
    child_pidfile = pathlib.Path(tempfile.mkdtemp()) / "capture-child.pid"
    stub = _script(
        "echo captured-out\n"
        "echo captured-err 1>&2\n"
        "sleep 30 &\n"
        f"echo $! > {shlex.quote(str(child_pidfile))}\n"
        "exit 0\n"
    )
    old_budget = cli._DRAIN_BUDGET
    cli._DRAIN_BUDGET = 0.05
    try:
        rc, out, err = await asyncio.wait_for(capture([stub], cwd="."), 2.0)
    finally:
        cli._DRAIN_BUDGET = old_budget

    assert rc == 0, rc
    assert "captured-out" in out, out
    assert "captured-err" in err, err
    child_pid = int(child_pidfile.read_text().strip())
    for _ in range(50):
        try:
            os.kill(child_pid, 0)
        except ProcessLookupError:
            break
        await asyncio.sleep(0.01)
    else:
        raise AssertionError(f"capture left descendant {child_pid} alive")
    print("PASS test_capture_surviving_child_does_not_hold_pipes_open")


async def _main() -> None:
    await test_stream_lines_live()
    await test_stream_lines_accepts_line_over_default_limit()
    await test_line_past_cap_is_yielded_in_bounded_pieces()
    await test_slow_reader_still_gets_final_line_after_fast_burst()
    await test_cancel_sigint_stops_before_completion()
    await test_capture_separates_stdout_stderr()
    await test_cancelled_capture_kills_its_process_group()
    await test_capture_surviving_child_does_not_hold_pipes_open()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
