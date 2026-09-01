"""Per-session ownership, cancellation, and cleanup for console chat turns.

This module owns only in-memory lifecycle state. It deliberately knows nothing
about message routing, event persistence, agents, or command semantics, keeping
the cancellation path usable by both the WebSocket facade and command runner.
"""

from __future__ import annotations

import asyncio
import contextlib
import typing as t
from dataclasses import dataclass, field
from datetime import datetime, timezone


@dataclass(slots=True)
class TurnState:
    """Typed ownership state for one admitted chat turn."""

    started_at: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )
    command: str | None = None
    cancelled: bool = False
    started: bool = False
    commands_starting: int = 0
    task: asyncio.Task[t.Any] | None = None


@dataclass(slots=True)
class SessionRuntime:
    """Every in-memory resource owned by one console session."""

    agent: t.Any = None
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    conn: t.Any = None
    turn: TurnState | None = None
    running: set[t.Any] = field(default_factory=set)
    # Kept true after deletion so stale WebSockets cannot recreate orphan data.
    closing: bool = False


# Strong refs keep background turns alive across client disconnects.
tasks: set[asyncio.Task[t.Any]] = set()
runtimes: dict[str, SessionRuntime] = {}


def runtime(session_id: str) -> SessionRuntime:
    """Return the session's runtime state, creating it on first use.

    Every other accessor here goes through this, so a caller never has to
    decide whether a session has been seen before.
    """
    current = runtimes.get(session_id)
    if current is None:
        current = SessionRuntime()
        runtimes[session_id] = current
    return current


def discard_if_idle(session_id: str, current: SessionRuntime) -> None:
    """Drop lock-only shells while retaining agents and deletion tombstones."""
    if (
        current.agent is None
        and current.conn is None
        and current.turn is None
        and not current.running
        and not current.closing
        and runtimes.get(session_id) is current
    ):
        runtimes.pop(session_id, None)


def active_turn(session_id: str) -> TurnState | None:
    """The running turn for a session, or None if it is idle."""
    current = runtimes.get(session_id)
    return current.turn if current is not None else None


def begin_cleanup(session_id: str) -> bool:
    """Atomically reserve an idle session against new turn dispatch."""
    current = runtime(session_id)
    if current.closing or current.turn is not None or current.running:
        return False
    current.closing = True
    return True


def release_cleanup(session_id: str) -> None:
    """Release a failed deletion reservation so the session remains usable."""
    current = runtimes.get(session_id)
    if current is not None:
        current.closing = False
        discard_if_idle(session_id, current)


def session_closing(session_id: str) -> bool:
    """Whether the session is mid-teardown and should refuse new work.

    Reads ``runtimes`` directly rather than via :func:`runtime` so merely
    asking the question cannot resurrect state for a session being evicted.
    """
    current = runtimes.get(session_id)
    return current.closing if current is not None else False


def register_conn(session_id: str, ws: t.Any) -> None:
    """Mark ``ws`` as the current socket for a session."""
    runtime(session_id).conn = ws


def unregister_conn(ws: t.Any) -> None:
    """Drop a closed socket from the registry."""
    for session_id, current in list(runtimes.items()):
        if current.conn is ws:
            current.conn = None
            discard_if_idle(session_id, current)


def cancel_session(session_id: str) -> bool:
    """Cancel the whole in-flight turn and every subprocess it owns."""
    current = runtimes.get(session_id)
    if current is None:
        return False
    turn = current.turn
    running = tuple(current.running)
    if turn is None and not running:
        return False

    if turn is not None:
        turn.cancelled = True
    for command in running:
        command.cancel()

    # A running subprocess unwinds first; its owner then observes cancellation.
    # Without one, interrupt model generation immediately. commands_starting
    # closes the race where cancellation lands during create_subprocess_exec.
    if not running and turn is not None and not turn.commands_starting:
        task = turn.task
        if task is not None and not task.done() and turn.started:
            task.cancel()
    return True


def session_lock(session_id: str) -> asyncio.Lock:
    """The per-session lock serialising turns, so one session runs one at a time."""
    return runtime(session_id).lock


async def cleanup_session(session_id: str, *, timeout: float = 15.0) -> None:
    """Stop and await one session, then evict all of its runtime state."""
    current = runtimes.get(session_id)
    if current is None:
        return
    turn = current.turn
    running = tuple(current.running)
    cancel_session(session_id)

    task = turn.task if turn is not None else None
    if task is not None and not task.done():
        done, _ = await asyncio.wait({task}, timeout=timeout)
        if not done:
            for command in tuple(current.running):
                force_kill = getattr(command, "force_kill", None)
                if force_kill is not None:
                    force_kill()
                else:
                    command.cancel()
            task.cancel()
            with contextlib.suppress(asyncio.CancelledError, Exception):
                await task
    elif running:
        # No owner task can reap these handles; do not leave them detached.
        for command in running:
            force_kill = getattr(command, "force_kill", None)
            if force_kill is not None:
                force_kill()

    current.agent = None
    current.conn = None
    current.turn = None
    current.running.clear()
    if not current.closing:
        runtimes.pop(session_id, None)


async def cleanup_all(*, timeout: float = 15.0) -> None:
    """Bounded shutdown cleanup for every in-memory session."""
    session_ids = set(runtimes)
    for current in runtimes.values():
        current.closing = True
    await asyncio.gather(
        *(cleanup_session(session_id, timeout=timeout) for session_id in session_ids)
    )
