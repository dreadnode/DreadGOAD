"""Multiplexed chat WebSocket (design §5.1, §7).

One socket carries a ``session_id`` on every message. Dispatch (§5.1):
  - ``dispatch="direct"`` commands (deterministic reads, /destroy) run the CLI
    programmatically via ``run_cli``;
  - ``dispatch="agent"`` commands expand to a structured prompt and run through
    the agent's ``run_dreadgoad`` tool — which calls the *same* ``run_cli``, so
    both paths stream/status/hook/cancel identically;
  - free-text goes to the agent.
All events are persisted to the event log and replayed on resume.

Live behavior needs an LLM key (OPENROUTER_API_KEY); the structural wiring is
import-verifiable without one.
"""

from __future__ import annotations

import asyncio
import json
import typing as t

from dreadnode.agent.events import (
    AgentEnd,
    AgentError,
    GenerationEnd,
    ToolEnd,
    ToolStart,
)

from . import commands, fetch, hook, paths
from .agent import create_agent
from .cli import start_command

# Chat-kind events replayed on resume (§6.3).
CHAT_KINDS = [
    "user_message",
    "generation",
    "tool_start",
    "tool_end",
    "error",
    "agent_end",
]

# Per-session agent runtime (isolated; §4.2). Keyed by session id.
_agents: dict[str, t.Any] = {}

# In-flight CLI commands, keyed by session id, for cancellation (§5.4).
_running: dict[str, t.Any] = {}

# Per-session serialization locks (same session = one turn at a time, §6.4).
_locks: dict[str, asyncio.Lock] = {}

# Strong refs to in-flight turn tasks so they aren't GC'd and survive a client
# disconnect (the op keeps running server-side, §5.4).
_tasks: set[asyncio.Task[t.Any]] = set()

# The current live WS for each session. Emits target this (not a socket captured
# at dispatch time), so a reconnected client re-attaches to an in-flight op's
# live stream + completion (§5.4). Updated on every message for the session.
_conns: dict[str, t.Any] = {}


def register_conn(session_id: str, ws: t.Any) -> None:
    """Mark ``ws`` as the current socket for a session (called per message)."""
    _conns[session_id] = ws


def unregister_conn(ws: t.Any) -> None:
    """Drop a closed socket from the registry (best-effort)."""
    for sid in [s for s, w in _conns.items() if w is ws]:
        del _conns[sid]


def cancel_session(session_id: str) -> bool:
    """Cancel the session's in-flight command (SIGINT). Returns True if one ran."""
    rc = _running.get(session_id)
    if rc is None:
        return False
    rc.cancel()
    return True


def _session_lock(session_id: str) -> asyncio.Lock:
    lock = _locks.get(session_id)
    if lock is None:
        lock = asyncio.Lock()
        _locks[session_id] = lock
    return lock


def dispatch(app: t.Any, session_id: str, content: str) -> asyncio.Task[t.Any]:
    """Run a turn in a background task, serialized per session (§6.4, §7).

    The WS recv loop calls this and immediately keeps reading, so `cancel` and
    other sessions' messages are handled while a long op streams (§5.4). The
    per-session lock makes same-session turns run one at a time; different
    sessions run concurrently. Tasks are kept in ``_tasks`` so they survive the
    connection closing; emits target the session's *current* socket (`_conns`).
    """

    async def _runner() -> None:
        async with _session_lock(session_id):
            await handle_message(app, session_id, content)

    task = asyncio.create_task(_runner())
    _tasks.add(task)
    task.add_done_callback(_tasks.discard)
    return task


def cleanup_session(session_id: str) -> None:
    """Drop per-session runtime state on delete (agent, lock, conn, command)."""
    _agents.pop(session_id, None)
    _locks.pop(session_id, None)
    _conns.pop(session_id, None)
    rc = _running.pop(session_id, None)
    if rc is not None:
        rc.cancel()


def format_event(event: t.Any) -> dict[str, t.Any] | None:
    """Convert a dreadnode AgentEvent to a JSON-able chat event (ALFRED shape)."""
    if isinstance(event, GenerationEnd):
        usage = None
        if event.usage:
            usage = {
                "input_tokens": event.usage.input_tokens,
                "output_tokens": event.usage.output_tokens,
            }
        return {
            "kind": "generation",
            "content": event.message.content or "",
            "usage": usage,
        }
    if isinstance(event, ToolStart):
        return {
            "kind": "tool_start",
            "tool": event.tool_call.name,
            "args": event.tool_call.function.arguments,
        }
    if isinstance(event, ToolEnd):
        return {
            "kind": "tool_end",
            "tool": event.tool_call.name,
            "result": (event.message.content or "")[:2000],
        }
    if isinstance(event, AgentError):
        return {"kind": "error", "message": str(event.error)}
    if isinstance(event, AgentEnd):
        return {"kind": "agent_end", "failed": event.result.failed}
    return None


async def _get_agent(app: t.Any, session_id: str) -> t.Any | None:
    if session_id in _agents:
        return _agents[session_id]
    session = await app.state.db.get_session(session_id)
    if session is None:
        return None
    agent = create_agent(
        session.get("model") or "openrouter/anthropic/claude-sonnet-5",
        session,
        app,
        session_id,
        run_cli,
    )
    _agents[session_id] = agent
    return agent


async def emit_event(
    app: t.Any,
    session_id: str,
    kind: str,
    payload: dict[str, t.Any],
    *,
    persist: bool = True,
) -> None:
    """Persist (optionally) + push an event to the session's current socket (§5.4)."""
    if persist:
        await app.state.db.append_event(session_id, kind, payload)
    ws = _conns.get(session_id)
    if ws is None:
        return  # no live client; persisted events replay on reconnect
    try:
        await ws.send_text(
            json.dumps({"session_id": session_id, "kind": kind, **payload})
        )
    except Exception:  # noqa: BLE001
        pass  # client dropped mid-send; op continues, events persisted


async def run_cli(
    app: t.Any, session_id: str, name: str, extra: list[str] | None = None
) -> tuple[int, str]:
    """Run a dreadgoad command through the full pipeline; return (exit_code, output).

    Emits command_run/command_progress/check_run + overlays, sets lifecycle
    status, registers ``_running`` for cancel. Shared by direct dispatch and the
    agent's ``run_dreadgoad`` tool, so both paths behave identically (§5.1).
    """
    db = app.state.db
    session = await db.get_session(session_id)
    if session is None:
        await emit_event(app, session_id, "error", {"message": "session not found"})
        return 1, "session not found"
    extra = list(extra or [])

    # /score: fetch the (remote) report into the session dir first (§5.2).
    if name == "/score" and extra:
        try:
            rc_fetch, local, msg = await fetch.fetch_report(
                session["snapshot"], session["session_dir"], extra[0]
            )
        except ValueError as exc:
            await emit_event(app, session_id, "error", {"message": str(exc)})
            return 1, str(exc)
        if rc_fetch != 0:
            await emit_event(
                app,
                session_id,
                "error",
                {"message": f"report fetch failed: {msg[-300:]}"},
            )
            return rc_fetch, msg
        extra = [local, *extra[1:]]

    argv = commands.build_argv(session, name, extra, repo_root=str(paths.repo_root()))
    await emit_event(
        app,
        session_id,
        "command_run",
        {"phase": "start", "command": name, "argv": argv},
    )

    cmd = commands.REGISTRY[name]
    if cmd.long_running:
        await app.state.sessions.set_status(session_id, "provisioning")

    rc = await start_command(argv, cwd=str(paths.repo_root()))
    _running[session_id] = rc
    try:
        async for line in rc.stream_lines():
            await emit_event(
                app, session_id, "command_progress", {"line": line}, persist=False
            )
    finally:
        _running.pop(session_id, None)
    exit_code = rc.returncode
    output = rc.output

    if cmd.long_running:
        if rc.cancelled:
            final = "interrupted"  # user cancel ≠ failure (§5.4)
        elif exit_code:
            final = "error"
        elif name == "/destroy":
            final = "destroyed"
        else:
            final = "running"
        await app.state.sessions.set_status(session_id, final)

    await emit_event(
        app,
        session_id,
        "command_run",
        {
            "phase": "end",
            "command": name,
            "exit_code": exit_code,
            "tail": output[-2000:],
        },
    )

    payload = await hook.run_check(app, session_id)
    await emit_event(app, session_id, "check_run", payload)

    # Command-specific overlays (§6.4 / §6.3).
    if name == "/health":
        await hook.apply_health(app, session_id, exit_code == 0)
    elif name in ("/variant", "/extensions"):
        await hook.reseed(app, session_id)

    return exit_code, output


async def handle_message(app: t.Any, session_id: str, content: str) -> None:
    """Route a message: direct command → run_cli; agent command / free-text → agent."""
    await emit_event(app, session_id, "user_message", {"content": content})

    if commands.is_command(content):
        name, extra = commands.parse_command(content)
        session = await app.state.db.get_session(session_id)
        if session is None:
            await emit_event(app, session_id, "error", {"message": "session not found"})
            await emit_event(app, session_id, "agent_end", {"failed": True})
            return
        if commands.REGISTRY[name].dispatch == "direct":
            # Direct commands are deterministic reads (+ /destroy) with a fixed
            # verb; extra operator tokens would land as bogus CLI args. Reject
            # cleanly instead of shelling out to a guaranteed CLI error (C4).
            if extra:
                await emit_event(
                    app,
                    session_id,
                    "error",
                    {"message": f"{name} takes no arguments (got: {' '.join(extra)})"},
                )
                await emit_event(app, session_id, "agent_end", {"failed": True})
                return
            exit_code, _ = await run_cli(app, session_id, name, extra)
            await emit_event(app, session_id, "agent_end", {"failed": exit_code != 0})
            return
        # dispatch="agent": expand to a structured prompt; the agent runs it via
        # its run_dreadgoad tool (robust arg interpretation, constrained).
        await _run_agent(app, session_id, commands.expand_command_prompt(name, extra))
        return

    await _run_agent(app, session_id, content)


async def _run_agent(app: t.Any, session_id: str, prompt: str) -> None:
    """Stream one agent turn (free-text or an expanded command) to the client."""
    agent = await _get_agent(app, session_id)
    if agent is None:
        await emit_event(app, session_id, "error", {"message": "session not found"})
        await emit_event(app, session_id, "agent_end", {"failed": True})
        return
    try:
        async with agent.stream(prompt) as events:
            async for event in events:
                formatted = format_event(event)
                if formatted:
                    kind = formatted.pop("kind")
                    await emit_event(app, session_id, kind, formatted)
    except Exception as exc:  # noqa: BLE001 - surface any agent error to the client
        await emit_event(app, session_id, "error", {"message": f"agent error: {exc}"})
        await emit_event(app, session_id, "agent_end", {"failed": True})


async def replay(app: t.Any, session_id: str) -> None:
    """Send chat-kind history for a session to its current socket on (re)connect."""
    events = await app.state.db.get_events(session_id, kinds=CHAT_KINDS)
    ws = _conns.get(session_id)
    if ws is None:
        return
    await ws.send_text(
        json.dumps({"session_id": session_id, "kind": "history", "events": events})
    )
