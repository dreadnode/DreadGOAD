"""Runner that shells out to the dreadgoad CLI (design §5.1, §5.4).

CLI commands run with ``cwd = repo root`` (they read ``ad/``, ``infra/``,
``dreadgoad.yaml``), streaming stdout line-by-line so long ops can surface a
live tail. The returned handle exposes cancellation (SIGINT) for §6.
"""

from __future__ import annotations

import asyncio
import os
import signal
import typing as t
from pathlib import Path

OnLine = t.Callable[[str], t.Any]


class RunningCommand:
    """A live CLI subprocess with a streamed-output future and cancel()."""

    # Grace after SIGINT before a hard SIGKILL. Long enough for terraform/ansible
    # to unwind on SIGINT, short enough that a command which *ignores* SIGINT
    # (e.g. a stuck health-check) still gets cancelled.
    _KILL_GRACE = 12.0

    def __init__(self, proc: asyncio.subprocess.Process) -> None:
        self._proc = proc
        self.lines: list[str] = []
        self.cancelled = False
        self._kill_task: asyncio.Task[None] | None = None

    def cancel(self) -> None:
        """Cancel the run: SIGINT the process group (so the CLI *and* its
        terraform/ansible children unwind gracefully), then escalate to SIGKILL
        if it hasn't exited within the grace period — some commands trap SIGINT
        (§5.4). The child is a group leader via ``start_new_session``."""
        if self._proc.returncode is not None:
            return
        self.cancelled = True
        self._killpg(signal.SIGINT)
        try:
            loop = asyncio.get_running_loop()
        except RuntimeError:
            return  # no loop → best-effort SIGINT only
        self._kill_task = loop.create_task(self._force_kill_after(self._KILL_GRACE))

    async def _force_kill_after(self, grace: float) -> None:
        await asyncio.sleep(grace)
        if self._proc.returncode is None:
            self._killpg(signal.SIGKILL)

    def _killpg(self, sig: int) -> None:
        with _suppress():
            os.killpg(os.getpgid(self._proc.pid), sig)

    @property
    def returncode(self) -> int:
        return self._proc.returncode or 0

    @property
    def output(self) -> str:
        return "\n".join(self.lines)

    async def stream_lines(self) -> t.AsyncIterator[str]:
        """Yield stdout lines as they arrive (live tail, §5.4), then reap."""
        assert self._proc.stdout is not None
        async for raw in self._proc.stdout:
            line = raw.decode("utf-8", errors="replace").rstrip("\n")
            self.lines.append(line)
            yield line
        await self._proc.wait()

    async def wait(self, on_line: OnLine | None = None) -> tuple[int, str]:
        """Stream stdout (merged stderr) until exit; return (rc, full_output)."""
        assert self._proc.stdout is not None
        async for raw in self._proc.stdout:
            line = raw.decode("utf-8", errors="replace").rstrip("\n")
            self.lines.append(line)
            if on_line is not None:
                on_line(line)
        await self._proc.wait()
        return self._proc.returncode or 0, "\n".join(self.lines)


class _suppress:
    def __enter__(self) -> None:
        return None

    def __exit__(self, *exc: object) -> bool:
        return True  # swallow ProcessLookupError etc.


async def start_command(argv: list[str], cwd: str | Path) -> RunningCommand:
    """Launch a CLI command (stdout+stderr merged) rooted at ``cwd``."""
    proc = await asyncio.create_subprocess_exec(
        *argv,
        cwd=str(cwd),
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.STDOUT,
        start_new_session=True,  # own process group → cancel() signals the whole tree
    )
    return RunningCommand(proc)


async def run_command(
    argv: list[str], cwd: str | Path, on_line: OnLine | None = None
) -> tuple[int, str]:
    """Convenience: start + wait. Returns (returncode, merged_output)."""
    rc = await start_command(argv, cwd)
    return await rc.wait(on_line)


async def capture(argv: list[str], cwd: str | Path) -> tuple[int, str, str]:
    """Run a command capturing stdout and stderr **separately**.

    Used for machine-readable output (e.g. ``lab status --json``): merging
    stderr into stdout would let a stray log/warning line corrupt JSON
    parsing. Returns (returncode, stdout, stderr).
    """
    proc = await asyncio.create_subprocess_exec(
        *argv,
        cwd=str(cwd),
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    out, err = await proc.communicate()
    return (
        proc.returncode or 0,
        out.decode("utf-8", errors="replace"),
        err.decode("utf-8", errors="replace"),
    )
