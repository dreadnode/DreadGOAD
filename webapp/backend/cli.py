"""Runner that shells out to the dreadgoad CLI (design §5.1, §5.4).

CLI commands run with ``cwd = repo root`` (they read ``ad/``, ``infra/``,
``dreadgoad.yaml``), streaming stdout line-by-line so long ops can surface a
live tail. The returned handle exposes cancellation (SIGINT) for §6.
"""

from __future__ import annotations

import asyncio
import signal
import typing as t
from pathlib import Path

OnLine = t.Callable[[str], t.Any]


class RunningCommand:
    """A live CLI subprocess with a streamed-output future and cancel()."""

    def __init__(self, proc: asyncio.subprocess.Process) -> None:
        self._proc = proc
        self.lines: list[str] = []

    def cancel(self) -> None:
        """Send SIGINT so the CLI can unwind gracefully (§5.4)."""
        if self._proc.returncode is None:
            with _suppress():
                self._proc.send_signal(signal.SIGINT)

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
