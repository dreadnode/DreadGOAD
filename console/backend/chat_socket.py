"""Bounded WebSocket protocol and multiplexed chat transport."""

from __future__ import annotations

import json
import re
import typing as t
from dataclasses import dataclass

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from . import approvals, auth, chat

router = APIRouter()

# Browser WebSockets do not enforce same-origin handshakes. Permit the console
# and dev proxy on loopback, while rejecting pages on other origins.
_WS_ORIGIN_RE = re.compile(
    r"^(?:https?|wss?)://(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$"
)

WS_MAX_CONTENT_CHARS = 32_768
WS_MAX_MESSAGE_CHARS = 65_536
WS_MAX_SESSION_ID_CHARS = 128

_COMMON_FIELDS = frozenset({"session_id", "type"})
_FIELDS_BY_TYPE = {
    "message": _COMMON_FIELDS | {"content"},
    "resume": _COMMON_FIELDS,
    "cancel": _COMMON_FIELDS,
    "approval": _COMMON_FIELDS | {"approval_id", "decision"},
}


@dataclass(frozen=True)
class _FrameEnvelope:
    """Validated fields shared by every client WebSocket frame."""

    session_id: str
    message_type: str


def ws_origin_allowed(origin: str | None) -> bool:
    """Return whether an Origin may open the console WebSocket."""
    if origin is None:
        return True
    return bool(_WS_ORIGIN_RE.match(origin.strip().lower()))


def _parse_envelope(
    value: dict[str, t.Any],
) -> tuple[_FrameEnvelope | None, str | None, str | None]:
    """Validate shared fields and return an envelope plus a safe session id."""
    raw_session_id = value.get("session_id")
    if not isinstance(raw_session_id, str) or not raw_session_id.strip():
        return None, "session_id must be a non-empty string", None
    session_id = raw_session_id.strip()
    if len(session_id) > WS_MAX_SESSION_ID_CHARS:
        return None, "session_id is too long", None

    raw_type = value.get("type", "message")
    if not isinstance(raw_type, str):
        return None, "type must be a string", session_id
    if raw_type not in _FIELDS_BY_TYPE:
        return None, f"unknown message type: {raw_type}", session_id
    return _FrameEnvelope(session_id, raw_type), None, session_id


def _unexpected_fields(value: dict[str, t.Any], message_type: str) -> str | None:
    """Describe fields that are not part of the selected frame type."""
    unexpected = sorted(set(value) - _FIELDS_BY_TYPE[message_type])
    if unexpected:
        return f"unexpected field(s): {', '.join(unexpected)}"
    return None


def _parse_message_payload(
    value: dict[str, t.Any],
) -> tuple[dict[str, str] | None, str | None]:
    """Validate and normalize the content carried by a message frame."""
    content = value.get("content")
    if not isinstance(content, str):
        return None, "content must be a string"
    content = content.strip()
    if not content:
        return None, "content must not be empty"
    if len(content) > WS_MAX_CONTENT_CHARS:
        return None, "content is too large"
    return {"content": content}, None


def _parse_approval_payload(
    value: dict[str, t.Any],
) -> tuple[dict[str, str] | None, str | None]:
    """Validate and normalize an approval decision."""
    approval_id = value.get("approval_id")
    if not isinstance(approval_id, str) or not approval_id.strip():
        return None, "approval_id must be a non-empty string"
    decision = value.get("decision")
    if decision not in {"confirm", "deny"}:
        return None, "decision must be 'confirm' or 'deny'"
    return {"approval_id": approval_id.strip(), "decision": decision}, None


def parse_ws_message(
    raw: str,
) -> tuple[dict[str, t.Any] | None, str | None, str | None]:
    """Validate a client frame and return message, error, and safe session id."""
    if len(raw) > WS_MAX_MESSAGE_CHARS:
        return None, "message is too large", None

    try:
        value = json.loads(raw)
    except (ValueError, RecursionError):
        return None, "message must be valid JSON", None
    if not isinstance(value, dict):
        return None, "message must be a JSON object", None

    envelope, error, session_id = _parse_envelope(value)
    if error is not None:
        return None, error, session_id
    assert envelope is not None and session_id is not None

    error = _unexpected_fields(value, envelope.message_type)
    if error is not None:
        return None, error, session_id

    payload: dict[str, str] = {}
    if envelope.message_type == "message":
        parsed, error = _parse_message_payload(value)
        if error is not None:
            return None, error, session_id
        assert parsed is not None
        payload = parsed
    elif envelope.message_type == "approval":
        parsed, error = _parse_approval_payload(value)
        if error is not None:
            return None, error, session_id
        assert parsed is not None
        payload = parsed

    return (
        {
            "session_id": envelope.session_id,
            "type": envelope.message_type,
            **payload,
        },
        None,
        session_id,
    )


@router.websocket("/ws/chat")
async def ws_chat(websocket: WebSocket) -> None:
    """Multiplex every session over one reconnectable WebSocket."""
    app = websocket.app
    if not ws_origin_allowed(websocket.headers.get("origin")):
        await websocket.close(code=1008)
        return
    bearer_ok = auth.bearer_authorized(websocket.headers.get("authorization"))
    protocol_ok = auth.websocket_protocol_authorized(
        websocket.headers.get("sec-websocket-protocol")
    )
    if not bearer_ok and not protocol_ok:
        await websocket.close(code=1008)
        return
    await websocket.accept(subprotocol=auth.WS_PROTOCOL if protocol_ok else None)
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
            if message["type"] == "approval":
                approval_error = approvals.resolve(
                    session_id,
                    message["approval_id"],
                    message["decision"] == "confirm",
                )
                if approval_error is not None:
                    await chat.emit_event(
                        app,
                        session_id,
                        "error",
                        {"message": approval_error},
                        persist=False,
                    )
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
