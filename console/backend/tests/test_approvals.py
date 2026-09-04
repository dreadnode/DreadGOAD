"""Tests for exact, single-use backend command approvals."""

from __future__ import annotations

import asyncio
import json
import types

from console.backend import approvals, chat_events, chat_runtime, command_runner


class _DB:
    def __init__(self) -> None:
        self.events: list[tuple[str, str, dict[str, object]]] = []

    async def append_event(
        self, session_id: str, kind: str, payload: dict[str, object]
    ) -> int:
        self.events.append((session_id, kind, payload))
        return len(self.events)

    async def prune_events(self, session_id: str) -> int:
        return 0

    async def get_events(
        self, session_id: str, kinds: list[str] | None = None
    ) -> list[dict[str, object]]:
        return []


def _app() -> tuple[object, _DB]:
    db = _DB()
    return types.SimpleNamespace(state=types.SimpleNamespace(db=db)), db


async def _pending(session_id: str) -> chat_runtime.PendingApproval:
    for _ in range(20):
        pending = chat_runtime.runtime(session_id).pending_approval
        if pending is not None:
            return pending
        await asyncio.sleep(0)
    raise AssertionError("approval was not registered")


async def test_approval_is_exact_single_use_and_audited() -> None:
    app, db = _app()
    sid = "s-approval"
    argv = ["dreadgoad", "--config", "/x", "--env", "dev", "up"]
    try:
        task = asyncio.create_task(approvals.require(app, sid, "/up", argv))
        pending = await _pending(sid)
        assert pending.argv == tuple(argv)
        assert approvals.resolve(sid, "wrong-id", True) is not None
        assert not task.done(), "a mismatched approval released the command"
        assert approvals.resolve(sid, pending.id, True) is None
        assert await task == (True, pending.id)
        assert approvals.resolve(sid, pending.id, True) is not None
        assert [kind for _, kind, _ in db.events] == [
            "approval_required",
            "approval_resolved",
        ]
        assert db.events[-1][2]["decision"] == "approved"
    finally:
        chat_runtime.runtimes.pop(sid, None)


async def test_approval_denial_and_timeout_fail_closed() -> None:
    app, db = _app()
    denied_sid = "s-denied"
    expired_sid = "s-expired"
    original_timeout = approvals.APPROVAL_TIMEOUT_SECONDS
    try:
        denied = asyncio.create_task(
            approvals.require(app, denied_sid, "/destroy", ["dreadgoad", "destroy"])
        )
        pending = await _pending(denied_sid)
        assert approvals.resolve(denied_sid, pending.id, False) is None
        assert await denied == (False, pending.id)

        approvals.APPROVAL_TIMEOUT_SECONDS = 0.001
        approved, approval_id = await approvals.require(
            app, expired_sid, "/up", ["dreadgoad", "up"]
        )
        assert approved is False and approval_id
        decisions = [
            payload["decision"]
            for _, kind, payload in db.events
            if kind == "approval_resolved"
        ]
        assert decisions == ["denied", "expired"]
    finally:
        approvals.APPROVAL_TIMEOUT_SECONDS = original_timeout
        chat_runtime.runtimes.pop(denied_sid, None)
        chat_runtime.runtimes.pop(expired_sid, None)


async def test_pending_approval_is_replayed_after_reconnect() -> None:
    app, _ = _app()
    sid = "s-reconnect"

    class _WS:
        sent: list[dict[str, object]] = []

        async def send_text(self, value: str) -> None:
            self.sent.append(json.loads(value))

    try:
        current = chat_runtime.runtime(sid)
        current.conn = _WS()
        current.pending_approval = chat_runtime.PendingApproval(
            id="approval-reconnect",
            command="/up",
            argv=("dreadgoad", "up"),
            detail="creates resources",
            requested_at="2026-01-01T00:00:00+00:00",
            decision=asyncio.get_running_loop().create_future(),
        )
        await chat_events.replay(app, sid)
        replayed = current.conn.sent[-1]["approval"]
        assert isinstance(replayed, dict)
        assert replayed["approval_id"] == "approval-reconnect"
        assert replayed["argv"] == ["dreadgoad", "up"]
    finally:
        chat_runtime.runtimes.pop(sid, None)


async def test_cancelling_session_denies_pending_approval() -> None:
    app, db = _app()
    sid = "s-cancel-approval"
    try:
        task = asyncio.create_task(
            approvals.require(app, sid, "/up", ["dreadgoad", "up"])
        )
        pending = await _pending(sid)

        assert chat_runtime.begin_cleanup(sid) is False
        assert chat_runtime.cancel_session(sid) is True
        assert await task == (False, pending.id)
        assert chat_runtime.runtime(sid).pending_approval is None
        resolved = [
            payload for _, kind, payload in db.events if kind == "approval_resolved"
        ]
        assert resolved[-1]["decision"] == "denied"
    finally:
        chat_runtime.runtimes.pop(sid, None)


async def test_command_runner_stops_before_spawn_when_approval_is_denied() -> None:
    class _RunnerDB(_DB):
        async def get_session(self, session_id: str) -> dict[str, object]:
            return {
                "anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"},
                "snapshot": {},
            }

    db = _RunnerDB()
    app = types.SimpleNamespace(state=types.SimpleNamespace(db=db))
    original_require = approvals.require
    original_preflight = command_runner.projectroot.preflight
    original_spawn = command_runner._spawn_and_stream

    async def deny(app_: object, sid: str, name: str, argv: list[str]):
        assert name == "/destroy"
        assert argv[-3:] == ["infra", "destroy", "--auto-approve"]
        return False, "approval-id"

    def preflight(*args: object, **kwargs: object):
        return types.SimpleNamespace(root="/x", warnings=[])

    async def must_not_spawn(*args: object, **kwargs: object) -> None:
        raise AssertionError("denied command continued toward process spawn")

    approvals.require = deny
    command_runner.projectroot.preflight = preflight
    command_runner._spawn_and_stream = must_not_spawn
    try:
        exit_code, output = await command_runner.run_cli(
            app, "s-runner-denied", "/destroy"
        )
        assert exit_code == 2 and "not run" in output
    finally:
        approvals.require = original_require
        command_runner.projectroot.preflight = original_preflight
        command_runner._spawn_and_stream = original_spawn


def test_unprotected_command_needs_no_runtime_or_approval() -> None:
    sid = "s-read"
    app, db = _app()
    assert approvals.REQUIRED_COMMANDS == {"/up", "/destroy"}
    approved, approval_id = asyncio.run(
        approvals.require(app, sid, "/instances", ["dreadgoad", "lab", "status"])
    )
    assert approved is True and approval_id is None
    assert sid not in chat_runtime.runtimes
    assert db.events == []
