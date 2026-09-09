"""Backend-enforced approval for high-consequence console commands."""

from __future__ import annotations

import asyncio
import contextlib
import secrets
import typing as t
from datetime import datetime, timezone

from . import chat_events, chat_runtime, commands

REQUIRED_COMMANDS = frozenset({"/up", "/destroy"})
APPROVAL_TIMEOUT_SECONDS = 300.0


async def require(
    app: t.Any, session_id: str, command: str, argv: list[str]
) -> tuple[bool, str | None]:
    """Wait for approval of exactly ``argv``; fail closed on timeout/cancel."""
    if command not in REQUIRED_COMMANDS:
        return True, None

    current = chat_runtime.runtime(session_id)
    if current.pending_approval is not None:
        raise RuntimeError("another command is already waiting for approval")

    loop = asyncio.get_running_loop()
    approval = chat_runtime.PendingApproval(
        id=secrets.token_urlsafe(24),
        command=command,
        argv=tuple(argv),
        detail=commands.REGISTRY[command].detail,
        requested_at=datetime.now(timezone.utc).isoformat(),
        decision=loop.create_future(),
    )
    current.pending_approval = approval

    outcome = "cancelled"
    try:
        await chat_events.emit_event(
            app, session_id, "approval_required", approval.public()
        )
        try:
            approved = await asyncio.wait_for(
                asyncio.shield(approval.decision), APPROVAL_TIMEOUT_SECONDS
            )
        except asyncio.TimeoutError:
            approved = False
            outcome = "expired"
        except asyncio.CancelledError:
            outcome = "cancelled"
            raise
        else:
            outcome = "approved" if approved else "denied"
        return approved, approval.id
    finally:
        if current.pending_approval is approval:
            current.pending_approval = None
        if not approval.decision.done():
            approval.decision.cancel()
        # Resolution is audit information, not authority. A transient storage or
        # socket failure here must not undo a decision or mask cancellation.
        with contextlib.suppress(Exception):
            await chat_events.emit_event(
                app,
                session_id,
                "approval_resolved",
                {
                    "approval_id": approval.id,
                    "command": command,
                    "decision": outcome,
                },
            )


def resolve(session_id: str, approval_id: str, approved: bool) -> str | None:
    """Resolve one live approval, returning an error instead of guessing."""
    current = chat_runtime.runtimes.get(session_id)
    approval = current.pending_approval if current is not None else None
    if approval is None:
        return "no command is waiting for approval"
    if not secrets.compare_digest(approval.id, approval_id):
        return "approval does not match the pending command"
    if approval.decision.done():
        return "approval has already been resolved"
    approval.decision.set_result(approved)
    return None
