"""Bounded WebSocket protocol and multiplexed chat transport."""

from __future__ import annotations

import json
import re

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from . import chat

router = APIRouter()

# Browser WebSockets do not enforce same-origin handshakes. Permit the console
# and dev proxy on loopback, while rejecting pages on other origins.
_WS_ORIGIN_RE = re.compile(
    r"^(?:https?|wss?)://(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$"
)

WS_MAX_CONTENT_CHARS = 32_768
WS_MAX_MESSAGE_CHARS = 65_536
WS_MAX_SESSION_ID_CHARS = 128


def ws_origin_allowed(origin: str | None) -> bool:
    """Return whether an Origin may open the console WebSocket."""
    if origin is None:
        return True
    return bool(_WS_ORIGIN_RE.match(origin.strip().lower()))


def parse_ws_message(
    raw: str,
) -> tuple[dict[str, str] | None, str | None, str | None]:
    """Validate a client frame and return message, error, and safe session id."""
    if len(raw) > WS_MAX_MESSAGE_CHARS:
        return None, "message is too large", None

    try:
        value = json.loads(raw)
    except json.JSONDecodeError:
        return None, "message must be valid JSON", None
    if not isinstance(value, dict):
        return None, "message must be a JSON object", None

    raw_session_id = value.get("session_id")
    if not isinstance(raw_session_id, str) or not raw_session_id.strip():
        return None, "session_id must be a non-empty string", None
    session_id = raw_session_id.strip()
    if len(session_id) > WS_MAX_SESSION_ID_CHARS:
        return None, "session_id is too long", None

    raw_type = value.get("type", "message")
    if not isinstance(raw_type, str):
        return None, "type must be a string", session_id
    message_type = raw_type
    if message_type not in {"message", "resume", "cancel"}:
        return None, f"unknown message type: {message_type}", session_id

    allowed = {"session_id", "type"}
    if message_type == "message":
        allowed.add("content")
    unexpected = sorted(set(value) - allowed)
    if unexpected:
        return (
            None,
            f"unexpected field(s): {', '.join(unexpected)}",
            session_id,
        )

    message = {"session_id": session_id, "type": message_type}
    if message_type == "message":
        content = value.get("content")
        if not isinstance(content, str):
            return None, "content must be a string", session_id
        content = content.strip()
        if not content:
            return None, "content must not be empty", session_id
        if len(content) > WS_MAX_CONTENT_CHARS:
            return None, "content is too large", session_id
        message["content"] = content
    return message, None, session_id


@router.websocket("/ws/chat")
async def ws_chat(websocket: WebSocket) -> None:
    """Multiplex every session over one reconnectable WebSocket."""
    app = websocket.app
    if not ws_origin_allowed(websocket.headers.get("origin")):
        await websocket.close(code=1008)
        return
    await websocket.accept()
    try:
        while True:
            raw = await websocket.receive_text()
            message, error, error_session_id = parse_ws_message(raw)
            if error is not None:
                payload = {"kind": "error", "message": error}
                if error_session_id is not None:
                    payload["session_id"] = error_session_id
                await websocket.send_text(json.dumps(payload))
                continue

            assert message is not None
            session_id = message["session_id"]
            chat.register_conn(session_id, websocket)
            if message["type"] == "resume":
                await chat.replay(app, session_id)
                continue
            if message["type"] == "cancel":
                chat.cancel_session(session_id)
                continue

            if chat.session_closing(session_id):
                await chat.emit_event(
                    app,
                    session_id,
                    "error",
                    {"message": "session is being deleted"},
                    persist=False,
                )
                continue
            if await app.state.db.get_session(session_id) is None:
                await websocket.send_text(
                    json.dumps(
                        {
                            "session_id": session_id,
                            "kind": "error",
                            "message": "session not found",
                        }
                    )
                )
                continue
            task = chat.dispatch(app, session_id, message["content"])
            if task is None:
                await chat.emit_event(
                    app,
                    session_id,
                    "error",
                    {"message": chat.TURN_BUSY_MESSAGE},
                    persist=False,
                )
    except WebSocketDisconnect:
        pass
    finally:
        chat.unregister_conn(websocket)
