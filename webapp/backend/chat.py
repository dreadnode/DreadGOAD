"""Multiplexed chat WebSocket (design §5.1, §7).

One socket carries a ``session_id`` on every message; the backend routes to
that session's agent (built lazily, kept per-session). Slash commands are
dispatched directly to the dreadgoad CLI (deterministic, streamed); free-text
is routed to the LLM agent. All events are persisted to the event log and
replayed on resume.

Live behavior needs an LLM key (OPENROUTER_API_KEY); the structural wiring is
import-verifiable without one.
"""

from __future__ import annotations

import json
import typing as t

from dreadnode.agent.events import (
    AgentEnd,
    AgentError,
    GenerationEnd,
    ToolEnd,
    ToolStart,
)

from . import commands, hook, paths
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


def cancel_session(session_id: str) -> bool:
    """Cancel the session's in-flight command (SIGINT). Returns True if one ran."""
    rc = _running.get(session_id)
    if rc is None:
        return False
    rc.cancel()
    return True


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
        str(paths.repo_root()),
    )
    _agents[session_id] = agent
    return agent


async def handle_message(app: t.Any, ws: t.Any, session_id: str, content: str) -> None:
    """Process one user message for a session: persist, dispatch, stream, persist."""
    db = app.state.db

    async def emit(
        kind: str, payload: dict[str, t.Any], *, persist: bool = True
    ) -> None:
        if persist:
            await db.append_event(session_id, kind, payload)
        await ws.send_text(
            json.dumps({"session_id": session_id, "kind": kind, **payload})
        )

    async def run_hook_and_emit() -> None:
        """Fire the ingestion hook, surface check_run inline (§6.4)."""
        payload = await hook.run_check(app, session_id)
        await emit("check_run", payload)

    await emit("user_message", {"content": content})

    # --- direct-dispatch slash commands ---
    if commands.is_command(content):
        name, extra = commands.parse_command(content)
        session = await db.get_session(session_id)
        if session is None:
            await emit("error", {"message": "session not found"})
            await emit("agent_end", {"failed": True})
            return
        argv = commands.build_argv(
            session, name, extra, repo_root=str(paths.repo_root())
        )
        await emit("command_run", {"phase": "start", "command": name, "argv": argv})

        cmd = commands.REGISTRY[name]
        if cmd.long_running:
            await app.state.sessions.set_status(session_id, "provisioning")

        rc = await start_command(argv, cwd=str(paths.repo_root()))
        _running[session_id] = rc
        try:
            # Live tail: stream stdout lines as they arrive (§5.4). Not persisted.
            async for line in rc.stream_lines():
                await emit("command_progress", {"line": line}, persist=False)
        finally:
            _running.pop(session_id, None)
        exit_code = rc.returncode
        output = rc.output

        if cmd.long_running:
            final = (
                "error"
                if exit_code
                else ("destroyed" if name == "/destroy" else "running")
            )
            await app.state.sessions.set_status(session_id, final)

        await emit(
            "command_run",
            {
                "phase": "end",
                "command": name,
                "exit_code": exit_code,
                "tail": output[-2000:],
            },
        )
        await run_hook_and_emit()

        # Command-specific overlays (§6.4 / §6.3).
        if name == "/health":
            await hook.apply_health(app, session_id, exit_code == 0)
        elif name in ("/variant", "/extensions"):
            await hook.reseed(app, session_id)

        await emit("agent_end", {"failed": exit_code != 0})
        return

    # --- free-text → LLM agent ---
    agent = await _get_agent(app, session_id)
    if agent is None:
        await emit("error", {"message": "session not found"})
        await emit("agent_end", {"failed": True})
        return
    try:
        async with agent.stream(content) as events:
            async for event in events:
                formatted = format_event(event)
                if formatted:
                    kind = formatted.pop("kind")
                    await emit(kind, formatted)
        await run_hook_and_emit()
    except Exception as exc:  # noqa: BLE001 - surface any agent error to the client
        await emit("error", {"message": f"agent error: {exc}"})
        await emit("agent_end", {"failed": True})


async def replay(app: t.Any, ws: t.Any, session_id: str) -> None:
    """Send chat-kind history for a session on (re)connect."""
    events = await app.state.db.get_events(session_id, kinds=CHAT_KINDS)
    await ws.send_text(
        json.dumps({"session_id": session_id, "kind": "history", "events": events})
    )
