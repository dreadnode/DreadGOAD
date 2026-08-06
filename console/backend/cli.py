"""Runner that shells out to the dreadgoad CLI (design §5.1, §5.4).

CLI commands run with ``cwd = repo root`` (they read ``ad/``, ``infra/``,
``dreadgoad.yaml``), streaming stdout line-by-line so long ops can surface a
live tail. The returned handle exposes cancellation (SIGINT) for §6.
"""

from __future__ import annotations

import asyncio
import os
import signal
import time
import typing as t
from pathlib import Path

OnLine = t.Callable[[str], t.Any]

# How often to check whether the process has exited while a read is pending.
# Only costs anything when output is idle; a ready line returns immediately.
_POLL_INTERVAL = 0.25

# Hard ceiling on how long to keep reading after the process has exited. Without
# it a *chatty* survivor (a tunnel logging on a timer) satisfies every read and
# streams forever — hanging the turn exactly as the original EOF wait did. Long
# enough to flush output the CLI itself had buffered at exit.
_DRAIN_BUDGET = 2.0

# How long to wait for exit after the pipe closes, before giving up on it.
_EXIT_GRACE = 30.0


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
        # Capture the process group now, while the child is certainly alive.
        # Resolving it later with getpgid() is unsafe: we also signal *after*
        # exit (see _reap_group), and a reaped PID can be recycled by the OS —
        # we would then signal an unrelated process's group. ``start_new_session``
        # makes the child its own group leader, so pgid == pid.
        try:
            self._pgid: int | None = os.getpgid(proc.pid)
        except OSError:
            self._pgid = None

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
        if self._pgid is None:
            return
        with _suppress():
            os.killpg(self._pgid, sig)

    @property
    def returncode(self) -> int:
        """Exit code, or 0 while the process is still running."""
        return self._proc.returncode or 0

    @property
    def output(self) -> str:
        """All output captured so far, joined with newlines."""
        return "\n".join(self.lines)

    def _record(self, raw: bytes) -> str:
        line = raw.decode("utf-8", errors="replace").rstrip("\n")
        self.lines.append(line)
        return line

    async def stream_lines(self) -> t.AsyncIterator[str]:
        """Yield stdout lines until the process **exits**, then drain and stop.

        Keyed on process exit, not pipe EOF. The CLI spawns helpers that inherit
        this stdout pipe and can outlive it — on Azure, ``health-check`` leaves
        an ``az network bastion tunnel`` running, which holds the write end open
        indefinitely. Waiting for EOF hangs the read forever, and since turns are
        serialized per session that wedges the entire chat for that session.
        """
        assert self._proc.stdout is not None
        stdout = self._proc.stdout
        # NOT `proc.wait()`: asyncio only resolves it once every pipe is closed
        # too, so a surviving child blocks it exactly like the raw read. The
        # transport sets `returncode` on real process exit, so poll that.
        line_task: asyncio.Future[bytes] | None = None
        # Set once exit is observed; bounds how long we keep draining after it.
        # Checked on *every* iteration, not just on read timeout — a survivor
        # that writes continuously (a tunnel logging on a timer) satisfies the
        # read every time and would otherwise stream forever.
        deadline: float | None = None
        try:
            while True:
                if line_task is None:
                    line_task = asyncio.ensure_future(stdout.readline())
                budget = _POLL_INTERVAL
                if deadline is not None:
                    budget = min(budget, deadline - time.monotonic())
                    if budget <= 0:
                        line_task.cancel()
                        break  # drain budget spent; the rest isn't ours
                try:
                    raw = await asyncio.wait_for(asyncio.shield(line_task), budget)
                except asyncio.TimeoutError:
                    if self._proc.returncode is not None and deadline is None:
                        deadline = time.monotonic() + _DRAIN_BUDGET
                    continue
                line_task = None
                if not raw:
                    break  # genuine EOF: nothing is holding the pipe
                yield self._record(raw)
                if self._proc.returncode is not None and deadline is None:
                    deadline = time.monotonic() + _DRAIN_BUDGET
        finally:
            if self._proc.returncode is None:
                # Pipe closed before exit — now wait() can actually resolve.
                try:
                    await asyncio.wait_for(self._proc.wait(), _EXIT_GRACE)
                except (asyncio.TimeoutError, asyncio.CancelledError):
                    self._killpg(signal.SIGKILL)
            self._reap_group()

    def _reap_group(self) -> None:
        """Terminate anything left in the child's process group.

        ``start_new_session`` gave the CLI its own group, so this only reaches
        our own descendants. Without it, helpers that outlive the CLI leak — we
        found orphaned bastion tunnels over four hours old — and keep the stdout
        pipe open for whoever reads it next.
        """
        if self._proc.returncode is None:
            return  # still running; not ours to reap yet
        self._killpg(signal.SIGTERM)

    async def wait(self, on_line: OnLine | None = None) -> tuple[int, str]:
        """Stream stdout (merged stderr) until exit; return (rc, full_output)."""
        async for line in self.stream_lines():
            if on_line is not None:
                on_line(line)
        return self.returncode, self.output


class _suppress:
    """Swallow every exception from the block.

    Used only around ``os.killpg``, where the interesting failures are benign
    races (ProcessLookupError, PermissionError) against a process that already
    exited. Deliberately broader than ``contextlib.suppress(ProcessLookupError)``
    so signalling can never take down a turn.
    """

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
