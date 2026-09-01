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
    await test_cancel_sigint_stops_before_completion()
    await test_capture_separates_stdout_stderr()
    await test_cancelled_capture_kills_its_process_group()
    await test_capture_surviving_child_does_not_hold_pipes_open()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
