"""Persistence and WebSocket delivery for console chat events."""

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

from . import chat_runtime

# Chat-kind events replayed on resume. Live progress and check notifications
# are deliberately absent: progress is transient and RangeView refreshes range
# state over REST after a check.
CHAT_KINDS = [
    "user_message",
    "generation",
    "tool_start",
    "tool_end",
    "error",
    "agent_end",
    "status",
    "instances_report",
    "health_report",
    "validate_report",
    "scrub_report",
    "exec_report",
    "security_report",
]


def format_agent_event(event: t.Any) -> dict[str, t.Any] | None:
    """Convert a dreadnode AgentEvent to the console's JSON event shape."""
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


async def emit_event(
    app: t.Any,
    session_id: str,
    kind: str,
    payload: dict[str, t.Any],
    *,
    persist: bool = True,
) -> None:
    """Persist an event and push it to the session's current socket."""
    if persist:
        await app.state.db.append_event(session_id, kind, payload)
    current = chat_runtime.runtimes.get(session_id)
    ws = current.conn if current is not None else None
    if ws is None:
        return
    try:
        await ws.send_text(
            json.dumps({"session_id": session_id, "kind": kind, **payload})
        )
    except Exception:  # noqa: BLE001
        # Client disconnects do not stop server-side work; persisted events are
        # replayed when another socket attaches.
        pass


def flatten_stored_event(event: dict[str, t.Any]) -> dict[str, t.Any]:
    """Reshape a stored event to the flat live WebSocket representation."""
    payload = event.get("payload") or {}
    return {
        "seq": event.get("seq"),
        "ts": event.get("ts"),
        "kind": event["kind"],
        **payload,
    }


MAX_REPLAY = 500


async def replay(app: t.Any, session_id: str) -> None:
    """Send persisted chat history and current turn state on reconnect."""
    await app.state.db.prune_events(session_id)
    events = await app.state.db.get_events(session_id, kinds=CHAT_KINDS)
    events = events[-MAX_REPLAY:]
    current = chat_runtime.runtimes.get(session_id)
    if current is None or current.conn is None:
        return
    ws = current.conn
    turn = current.turn
    await ws.send_text(
        json.dumps(
            {
                "session_id": session_id,
                "kind": "history",
                "events": [flatten_stored_event(event) for event in events],
                "active": turn is not None,
                "started_at": turn.started_at if turn else None,
                "command": turn.command if turn else None,
            }
        )
    )
