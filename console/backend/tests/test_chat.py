"""Tests for the chat runtime (Phase 3: T3.1) — the F1/F2/F3 fixes.

Covers: direct-dispatch flow (emit sequence + event persistence), single-turn
admission vs cross-session concurrency, and cleanup_session eviction.

Standalone:  python console/backend/tests/test_chat.py
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import os
import pathlib
import sys
import tempfile
import types
from contextlib import asynccontextmanager

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))
os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = tempfile.mkdtemp(prefix="dg-chat-")

from console.backend import (  # noqa: E402
    agent,
    chat,
    chat_runtime,
    command_runner,
    hook,
)
from console.backend.db import Database  # noqa: E402
from console.backend.sessions import SessionService  # noqa: E402
from dreadnode.agent.tools import FunctionCall, ToolCall  # noqa: E402

_REPO = pathlib.Path(__file__).resolve().parents[3]
_YAML = (
    "provider: aws\nregion: us-west-2\nenvironments:\n"
    "  dev:\n    variant_source: ad/DOES-NOT-EXIST\n"
    '    variant_target: ad/DOES-NOT-EXIST\n    vpc_cidr: "10.0.0.0/16"\n'
)


class FakeWS:
    def __init__(self) -> None:
        self.sent: list[dict] = []

    async def send_text(self, data: str) -> None:
        self.sent.append(json.loads(data))


class FakeRC:
    """Stand-in for cli.RunningCommand (no real subprocess)."""

    def __init__(self, lines: list[str], rc: int = 0) -> None:
        self._lines = lines
        self._rc = rc
        self.cancelled = False

    async def stream_lines(self):
        for line in self._lines:
            if self.cancelled:
                return
            await asyncio.sleep(0)
            yield line

    @property
    def returncode(self) -> int:
        return 130 if self.cancelled else self._rc

    @property
    def output(self) -> str:
        return "\n".join(self._lines)

    def cancel(self) -> None:
        self.cancelled = True


async def test_direct_dispatch_emits_and_persists() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))

        async def fake_start(argv, cwd):  # noqa: ANN001
            return FakeRC(["line-1", "line-2"])

        async def fake_check(a, sid_, capture_command=None):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = command_runner.start_command, hook.run_check
        command_runner.start_command = fake_start
        hook.run_check = fake_check
        try:
            s = await svc.create_session(str(cfg), "dev")
            ws = FakeWS()
            chat.register_conn(s["id"], ws)
            # /validate streams its output as a live tail.
            await chat.handle_message(app, s["id"], "/validate")

            kinds = [m["kind"] for m in ws.sent]
            assert kinds[0] == "user_message", kinds
            assert kinds[-1] == "agent_end", kinds
            assert "command_progress" in kinds, kinds
            assert "check_run" in kinds, kinds

            # command_progress is live-only and must NOT be persisted (§5.4).
            ekinds = [e["kind"] for e in await db.get_events(s["id"])]
            assert "user_message" in ekinds and "agent_end" in ekinds, ekinds
            assert "command_progress" not in ekinds, "progress must not persist"

            # /instances is the exception: its raw JSON is suppressed and
            # rendered as a formatted instances_report instead (§6.4).
            async def fake_json_start(argv, cwd):  # noqa: ANN001
                return FakeRC(
                    json.dumps([{"name": "dc01", "state": "running"}]).splitlines()
                )

            command_runner.start_command = fake_json_start
            ws.sent.clear()
            await chat.handle_message(app, s["id"], "/instances")
            kinds = [m["kind"] for m in ws.sent]
            assert "command_progress" not in kinds, "raw JSON must not stream"
            assert "instances_report" in kinds, kinds
            print("PASS test_direct_dispatch_emits_and_persists")
        finally:
            command_runner.start_command, hook.run_check = orig_start, orig_check
            await db.close()


async def test_start_failure_finishes_turn_and_restores_status() -> None:
    """A missing CLI must fail visibly, not strand status at provisioning."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))

        async def missing_cli(argv, cwd):  # noqa: ANN001
            raise FileNotFoundError(2, "No such file or directory", argv[0])

        async def check_must_not_run(a, sid_):  # noqa: ANN001
            raise AssertionError("post-command check ran even though nothing started")

        orig_start, orig_check = command_runner.start_command, hook.run_check
        command_runner.start_command, hook.run_check = missing_cli, check_must_not_run
        session_id: str | None = None
        try:
            s = await svc.create_session(str(cfg), "dev")
            sid = str(s["id"])
            session_id = sid  # tracked separately for the finally cleanup
            ws = FakeWS()
            chat.register_conn(sid, ws)

            # /health is long-running, so it first moves the session into
            # provisioning. A launch failure must move it back out again.
            await chat.handle_message(app, sid, "/health")

            current = await db.get_session(sid)
            assert current is not None and current["status"] == "error", current
            assert not chat_runtime.runtime(sid).running, (
                "a failed spawn cannot be cancellable"
            )

            kinds = [m["kind"] for m in ws.sent]
            assert kinds[-1] == "agent_end", kinds
            assert "error" in kinds, kinds
            ends = [
                m
                for m in ws.sent
                if m["kind"] == "command_run" and m.get("phase") == "end"
            ]
            assert len(ends) == 1 and ends[0]["exit_code"] == 1, ends
            assert ends[0]["cancelled"] is False, ends[0]
            assert "failed to start /health" in ends[0]["tail"], ends[0]
            assert ws.sent[-1]["failed"] is True, ws.sent[-1]
            print("PASS test_start_failure_finishes_turn_and_restores_status")
        finally:
            command_runner.start_command, hook.run_check = orig_start, orig_check
            if session_id is not None:
                await chat.cleanup_session(session_id)
            await db.close()


def test_final_status_precedence() -> None:
    """Lifecycle status for a finished long-running command (pure).

    Extracted from run_cli, where this branching was inlined and untestable.
    """
    fs = command_runner.final_status
    # A user cancel is never a failure, whatever the command or exit code.
    assert fs("/up", 1, True) == "interrupted"
    assert fs("/destroy", 0, True) == "interrupted"
    assert fs("/health", 1, True) == "interrupted"
    # /health exiting non-zero means checks failed, not that the range broke.
    assert fs("/health", 1, False) == "running"
    assert fs("/health", 0, False) == "running"
    # Any other non-zero exit is a range error.
    assert fs("/up", 1, False) == "error"
    assert fs("/destroy", 2, False) == "error", "a failed destroy is not 'destroyed'"
    # Clean runs.
    assert fs("/destroy", 0, False) == "destroyed"
    assert fs("/up", 0, False) == "running"
    assert fs("/provision", 0, False) == "running"
    print("PASS test_final_status_precedence")


async def test_replay_matches_live_event_shape() -> None:
    """Replayed history must be flat like live events, or the UI renders blank.

    Live emits ``{kind, **payload}``; the DB stores ``{seq, kind, ts, payload}``.
    Sending the stored shape put every field the client reads one level down.
    """
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        s = await svc.create_session(str(cfg), "dev")
        sid = s["id"]
        try:
            await db.append_event(sid, "user_message", {"content": "hello"})
            await db.append_event(
                sid, "instances_report", {"instances": [{"name": "dc01"}], "total": 1}
            )
            await db.append_event(sid, "health_report", {"passed": 2, "checks": []})
            # Live-only / REST-backed kinds must stay out of the replay.
            await db.append_event(sid, "check_run", {"hosts_updated": 3})

            app = types.SimpleNamespace(
                state=types.SimpleNamespace(db=db, sessions=svc)
            )
            ws = FakeWS()
            chat.register_conn(sid, ws)
            await chat.replay(app, sid)

            assert len(ws.sent) == 1 and ws.sent[0]["kind"] == "history", ws.sent
            events = ws.sent[0]["events"]
            kinds = [e["kind"] for e in events]
            assert "check_run" not in kinds, kinds

            by_kind = {e["kind"]: e for e in events}
            # Flat: fields sit on the event, not under a "payload" key.
            assert by_kind["user_message"]["content"] == "hello", by_kind
            assert "payload" not in by_kind["user_message"], "payload must be flattened"
            assert by_kind["instances_report"]["total"] == 1, by_kind
            assert by_kind["instances_report"]["instances"][0]["name"] == "dc01"
            assert by_kind["health_report"]["passed"] == 2, by_kind
            print("PASS test_replay_matches_live_event_shape")
        finally:
            await chat.cleanup_session(sid)
            await db.close()


async def test_resume_reports_in_flight_turn() -> None:
    """A reconnecting client must learn a turn is still running.

    Turns survive a disconnect by design, so without this a reloaded browser
    shows an idle pane mid-deploy — no working indicator, no cancel affordance.
    """
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        s = await svc.create_session(str(cfg), "dev")
        sid = s["id"]

        gate = asyncio.Event()

        async def blocking(app_, sid_, content):  # noqa: ANN001
            await gate.wait()

        orig = chat.handle_message
        chat.handle_message = blocking
        try:
            # Idle: resume says so.
            ws = FakeWS()
            chat.register_conn(sid, ws)
            await chat.replay(app, sid)
            assert ws.sent[0]["active"] is False, ws.sent[0]
            assert ws.sent[0]["started_at"] is None, ws.sent[0]

            # Start a turn and let it reach the lock, then "reload" the client.
            task = chat.dispatch(app, sid, "/up")
            assert task is not None, "an idle session must accept a turn"
            await asyncio.sleep(0.05)

            turn = chat.active_turn(sid)
            assert turn is not None, "turn should be registered while running"
            turn.command = "/up"  # normally set by run_cli

            ws2 = FakeWS()
            chat.register_conn(sid, ws2)
            await chat.replay(app, sid)
            hist = ws2.sent[0]
            assert hist["active"] is True, hist
            assert hist["command"] == "/up", hist
            assert hist["started_at"], "a start time is needed for the elapsed timer"
            # Parseable by the client's Date.parse.
            from datetime import datetime

            datetime.fromisoformat(hist["started_at"])

            # Finish: the flag clears.
            gate.set()
            await task
            assert chat.active_turn(sid) is None, "turn must not linger after it ends"
            ws3 = FakeWS()
            chat.register_conn(sid, ws3)
            await chat.replay(app, sid)
            assert ws3.sent[0]["active"] is False, ws3.sent[0]
            print("PASS test_resume_reports_in_flight_turn")
        finally:
            chat.handle_message = orig
            await chat.cleanup_session(sid)
            await db.close()


async def test_turn_flag_cleared_when_turn_raises() -> None:
    """A crashing turn must not strand the 'running' flag forever."""
    orig = chat.handle_message

    async def boom(app_, sid_, content):  # noqa: ANN001
        raise RuntimeError("turn blew up")

    chat.handle_message = boom
    try:
        task = chat.dispatch(None, "s-boom", "hi")
        assert task is not None, "an idle session must accept a turn"
        with contextlib.suppress(RuntimeError):
            await task
        assert chat.active_turn("s-boom") is None, "finally must clear the flag"
        print("PASS test_turn_flag_cleared_when_turn_raises")
    finally:
        chat.handle_message = orig
        await chat.cleanup_session("s-boom")


async def test_reattach_targets_current_conn() -> None:
    """After a reconnect, emits go to the session's *current* socket (F3)."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))

        async def fake_start(argv, cwd):  # noqa: ANN001
            return FakeRC(["l1"])

        async def fake_check(a, sid_, capture_command=None):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = command_runner.start_command, hook.run_check
        command_runner.start_command, hook.run_check = fake_start, fake_check
        try:
            s = await svc.create_session(str(cfg), "dev")
            ws1, ws2 = FakeWS(), FakeWS()
            chat.register_conn(s["id"], ws1)  # first connection
            chat.register_conn(s["id"], ws2)  # reconnect → ws2 is now current
            await chat.handle_message(app, s["id"], "/instances")
            assert ws2.sent, "current socket should receive events"
            assert not ws1.sent, "stale socket must NOT receive events"
            print("PASS test_reattach_targets_current_conn")
        finally:
            command_runner.start_command, hook.run_check = orig_start, orig_check
            await db.close()


async def test_dispatch_rejects_queued_turn_and_allows_concurrency() -> None:
    """Same-session extras are rejected; different sessions overlap (§6.4/§7)."""
    order: list[str] = []
    gate = asyncio.Event()

    async def fake_handle(app, sid, content):  # noqa: ANN001
        order.append(f"start:{sid}:{content}")
        if sid == "A":
            await gate.wait()
        else:
            await asyncio.sleep(0.02)
        order.append(f"end:{sid}:{content}")

    orig = chat.handle_message
    chat.handle_message = fake_handle
    try:
        # Admission is synchronous: the second message is rejected even before
        # the first task gets CPU time, rather than sitting behind its lock.
        first = chat.dispatch(None, "A", "1")
        queued = chat.dispatch(None, "A", "2")
        assert first is not None
        assert queued is None, "a second same-session turn must not be queued"
        await asyncio.sleep(0)
        assert order == ["start:A:1"], order
        gate.set()
        await first
        assert order == ["start:A:1", "end:A:1"], order

        # Completion releases admission for the next deliberate turn.
        next_turn = chat.dispatch(None, "A", "3")
        assert next_turn is not None
        await next_turn
        assert order[-2:] == ["start:A:3", "end:A:3"], order

        # different sessions → overlap
        order.clear()
        x = chat.dispatch(None, "X", "1")
        y = chat.dispatch(None, "Y", "1")
        assert x is not None and y is not None
        await asyncio.gather(x, y)
        assert order[:2] in (
            ["start:X:1", "start:Y:1"],
            ["start:Y:1", "start:X:1"],
        ), order
        print("PASS test_dispatch_rejects_queued_turn_and_allows_concurrency")
    finally:
        chat.handle_message = orig


class FakeAgent:
    """Records the prompt it was streamed; yields no events."""

    def __init__(self) -> None:
        self.prompt: str | None = None

    def stream(self, prompt: str):  # noqa: ANN201
        self.prompt = prompt
        return self._cm()

    @asynccontextmanager
    async def _cm(self):  # noqa: ANN202
        async def _events():
            return
            yield  # pragma: no cover — makes this an async generator

        yield _events()


async def test_agent_command_routes_to_agent() -> None:
    """A dispatch='agent' command becomes an expanded prompt to the agent (not direct)."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        s = await svc.create_session(str(cfg), "dev")
        ws = FakeWS()
        chat.register_conn(s["id"], ws)

        fake = FakeAgent()
        orig = chat._get_agent

        async def fake_get_agent(a, sid):  # noqa: ANN001, ANN202
            return fake

        chat._get_agent = fake_get_agent
        try:
            await chat.handle_message(app, s["id"], "/up using the variant at /p")
            assert fake.prompt is not None, "agent was not invoked"
            assert "run_dreadgoad" in fake.prompt and "/up" in fake.prompt, fake.prompt
            assert "using the variant at /p" in fake.prompt, fake.prompt
            # It must NOT direct-dispatch (no command_run emitted here).
            kinds = [m["kind"] for m in ws.sent]
            assert "command_run" not in kinds, (
                f"agent command must not direct-dispatch: {kinds}"
            )
            print("PASS test_agent_command_routes_to_agent")
        finally:
            chat._get_agent = orig
            await db.close()


async def test_run_dreadgoad_tool_validates_and_runs() -> None:
    calls: list[tuple[str, list[str]]] = []

    async def fake_run_cli(app, sid, cmd, args):  # noqa: ANN001, ANN202
        calls.append((cmd, args))
        return (0, "output tail")

    tool = agent._make_run_dreadgoad(object(), "s-1", fake_run_cli)

    # an action command → routed through run_cli
    out = await tool.fn(command="/up", args=["--from", "x"])
    assert calls == [("/up", ["--from", "x"])], calls
    assert "succeeded" in out, out

    # a read command → also runnable by the agent now
    calls.clear()
    await tool.fn(command="/instances", args=[])
    assert calls == [("/instances", [])], calls

    # /destroy is now agent-runnable too (safety is by prompt)
    calls.clear()
    await tool.fn(command="/destroy", args=[])
    assert calls == [("/destroy", [])], calls

    # an unknown command → refused, run_cli NOT called
    calls.clear()
    refused = await tool.fn(command="/bogus", args=[])
    assert "Refused" in refused, refused
    assert calls == [], "run_cli must not run for an unknown command"

    # The real tool execution wrapper catches ordinary exceptions for the
    # model, but must let turn cancellation escape instead of returning a
    # retryable tool error.
    async def cancelled_run_cli(app, sid, cmd, args):  # noqa: ANN001, ANN202
        raise asyncio.CancelledError

    cancelled_tool = agent._make_run_dreadgoad(object(), "s-1", cancelled_run_cli)
    call = ToolCall(
        id="cancel-1",
        function=FunctionCall(
            name="run_dreadgoad",
            arguments='{"command":"/health","args":[]}',
        ),
    )
    try:
        await cancelled_tool.handle_tool_call(call)
    except asyncio.CancelledError:
        pass
    else:
        raise AssertionError("tool wrapper converted cancellation into a model result")
    print("PASS test_run_dreadgoad_tool_validates_and_runs")


async def test_direct_command_rejects_extra_args() -> None:
    """A direct command with trailing tokens errors cleanly, never runs the CLI (C4)."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        s = await svc.create_session(str(cfg), "dev")
        ws = FakeWS()
        chat.register_conn(s["id"], ws)

        called = False

        async def fake_run_cli(a, sid, name, extra):  # noqa: ANN001, ANN202
            nonlocal called
            called = True
            return (0, "")

        orig = chat.run_cli
        chat.run_cli = fake_run_cli
        try:
            await chat.handle_message(app, s["id"], "/instances winterfell")
            assert not called, "run_cli must NOT run for a direct command with args"
            kinds = [m["kind"] for m in ws.sent]
            assert "error" in kinds and kinds[-1] == "agent_end", kinds
            assert "command_run" not in kinds, kinds
            err = next(m for m in ws.sent if m["kind"] == "error")
            assert "takes no arguments" in err["message"], err
            print("PASS test_direct_command_rejects_extra_args")
        finally:
            chat.run_cli = orig
            await db.close()


def test_instructions_renders_system_prompt() -> None:
    """_instructions loads prompts/system.md and fills $placeholders (no leftovers)."""
    session = {
        "anchor": {"config_path": "/x/dreadgoad.yaml", "env": "prod"},
        "snapshot": {"provider": "aws", "lab": "GOAD"},
    }
    text = agent._instructions(session)
    assert "/x/dreadgoad.yaml" in text and "prod" in text, text
    assert "aws" in text and "GOAD" in text, text
    assert "run_dreadgoad" in text, "system prompt must describe the tool"
    # every $placeholder must be substituted
    assert "$config_path" not in text and "$env" not in text, text
    assert "$provider" not in text and "$lab" not in text, text
    print("PASS test_instructions_renders_system_prompt")


async def test_health_emits_report_and_suppresses_json() -> None:
    """/health emits a structured health_report, not raw JSON, and stays running."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))

        checks = [
            {"name": "DC01 DC", "host": "DC01", "status": "OK", "detail": "DC01"},
            {
                "name": "DC02 Trust",
                "host": "DC02",
                "status": "FAIL",
                "detail": "no trust",
            },
        ]
        report = {"passed": 1, "failed": 1, "skipped": 0, "checks": checks}
        # NDJSON stream: requireInfra's "credentials OK", a compact line per
        # check (streamed live), then the final report line (parsed for the table).
        lines = ["  aws credentials OK (arn:aws:iam::1:user/x)"]
        lines += [json.dumps(c) for c in checks]
        lines.append(json.dumps(report))

        async def fake_start(argv, cwd):  # noqa: ANN001
            return FakeRC(lines, rc=1)  # exit 1: a check failed

        async def fake_check(a, sid_, capture_command=None):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = command_runner.start_command, hook.run_check
        command_runner.start_command, hook.run_check = fake_start, fake_check
        try:
            s = await svc.create_session(str(cfg), "dev")
            ws = FakeWS()
            chat.register_conn(s["id"], ws)
            await chat.handle_message(app, s["id"], "/health")

            kinds = [m["kind"] for m in ws.sent]
            assert "health_report" in kinds, kinds
            # per-check progress streams live (one readable line per check)
            progs = [m["line"] for m in ws.sent if m["kind"] == "command_progress"]
            assert len(progs) == 2, progs
            assert any("DC01 DC" in p for p in progs), progs
            # the raw report JSON line is NOT streamed
            assert not any('"checks"' in p for p in progs), progs
            hr = next(m for m in ws.sent if m["kind"] == "health_report")
            assert hr["passed"] == 1 and hr["failed"] == 1, hr
            assert len(hr["checks"]) == 2, hr

            # A failing /health must NOT mark the session errored (§ status fix).
            sess = await db.get_session(s["id"])
            assert sess is not None
            assert sess["status"] == "running", sess["status"]
            print("PASS test_health_emits_report_and_suppresses_json")
        finally:
            command_runner.start_command, hook.run_check = orig_start, orig_check
            await db.close()


class FakeThreadAgent:
    """Agent stand-in exposing a mutable ``thread.messages`` (for swap tests)."""

    def __init__(self, messages: list | None = None) -> None:
        self.thread = types.SimpleNamespace(messages=messages or [])


async def test_swap_model_preserves_thread_and_persists() -> None:
    """Switching model rebuilds the agent with history grafted + persists model."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        s = await svc.create_session(str(cfg), "dev")
        ws = FakeWS()
        chat.register_conn(s["id"], ws)

        old = FakeThreadAgent(["msg-1", "msg-2"])
        chat_runtime.runtime(s["id"]).agent = old
        seen = {}

        def fake_create(model, session, app_, sid_, run_cli):  # noqa: ANN001, ANN202
            seen["model"] = model
            return FakeThreadAgent([])  # fresh agent, empty thread

        orig = chat.create_agent
        chat.create_agent = fake_create
        try:
            out = await chat.swap_model(app, s["id"], "openrouter/x/y")
            assert out is not None
            # model persisted on the session
            sess = await db.get_session(s["id"])
            assert sess is not None and sess["model"] == "openrouter/x/y", sess
            # rebuilt with the new model, old conversation grafted on
            assert seen["model"] == "openrouter/x/y"
            new = chat_runtime.runtime(s["id"]).agent
            assert new is not old, "agent must be rebuilt"
            assert new.thread.messages == ["msg-1", "msg-2"], "history must carry over"
            # a status event announces the change
            assert any(
                m["kind"] == "status" and "Model changed" in (m.get("content") or "")
                for m in ws.sent
            ), ws.sent
            print("PASS test_swap_model_preserves_thread_and_persists")
        finally:
            chat.create_agent = orig
            chat_runtime.runtime(s["id"]).agent = None
            await db.close()


async def test_swap_model_no_live_agent() -> None:
    """With no cached agent, swap just persists the model (built fresh next turn)."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        s = await svc.create_session(str(cfg), "dev")
        chat_runtime.runtime(s["id"]).agent = None  # ensure not cached

        called = False

        def fake_create(*a, **k):  # noqa: ANN002, ANN003, ANN202
            nonlocal called
            called = True
            return FakeThreadAgent()

        orig = chat.create_agent
        chat.create_agent = fake_create
        try:
            out = await chat.swap_model(app, s["id"], "m2")
            assert out is not None
            assert not called, "no cached agent → must not build one eagerly"
            assert chat_runtime.runtime(s["id"]).agent is None
            sess = await db.get_session(s["id"])
            assert sess is not None and sess["model"] == "m2", sess
            # unknown session → None
            assert await chat.swap_model(app, "ghost", "m3") is None
            print("PASS test_swap_model_no_live_agent")
        finally:
            chat.create_agent = orig
            await db.close()


async def test_cleanup_session_evicts() -> None:
    runtime = chat_runtime.runtime("z")
    runtime.agent = object()
    command = FakeRC([])
    runtime.running.add(command)
    await chat.cleanup_session("z")
    assert "z" not in chat_runtime.runtimes, "runtime not evicted"
    assert command.cancelled, "cleanup must cancel the session's commands"
    print("PASS test_cleanup_session_evicts")


def test_cleanup_reservation_is_exclusive() -> None:
    sid = "s-cleanup-reservation"
    assert chat.begin_cleanup(sid) is True
    assert chat.begin_cleanup(sid) is False, "two deletes reserved one session"
    assert chat.dispatch(None, sid, "late turn") is None
    chat.release_cleanup(sid)
    print("PASS test_cleanup_reservation_is_exclusive")


async def test_cancel_session_cancels_every_parallel_command() -> None:
    """One Esc owns the turn, including parallel tools launched by the agent."""
    sid = "s-parallel-cancel"
    first, second = FakeRC([]), FakeRC([])
    runtime = chat_runtime.runtime(sid)
    runtime.turn = chat_runtime.TurnState()
    runtime.running.update({first, second})
    try:
        assert chat.cancel_session(sid) is True
        assert first.cancelled and second.cancelled
        assert runtime.turn is not None and runtime.turn.cancelled is True
        assert chat.cancel_session("s-idle") is False
        print("PASS test_cancel_session_cancels_every_parallel_command")
    finally:
        chat_runtime.runtimes.pop(sid, None)


async def test_captured_helper_is_owned_and_cancelled_with_turn() -> None:
    """Esc reaches post-command helpers that capture separate output streams."""

    class FakeCapture:
        def __init__(self) -> None:
            self.cancelled = False
            self.stopped = asyncio.Event()

        async def communicate(self) -> tuple[int, str, str]:
            await self.stopped.wait()
            return -2, "", ""

        def cancel(self) -> None:
            self.cancelled = True

        def force_kill(self) -> None:
            self.cancel()
            self.stopped.set()

    sid = "s-captured-helper"
    command = FakeCapture()
    spawned = asyncio.Event()
    release_spawn = asyncio.Event()

    async def fake_start(argv, cwd):  # noqa: ANN001
        spawned.set()
        await release_spawn.wait()
        return command

    original = command_runner.start_capture
    command_runner.start_capture = fake_start
    runtime = chat_runtime.runtime(sid)
    turn = chat_runtime.TurnState(started=True)
    runtime.turn = turn
    try:
        task = asyncio.create_task(
            command_runner._capture_for_turn(sid, ["helper"], ".")
        )
        turn.task = task
        await spawned.wait()

        # Esc during create_subprocess_exec must reserve cancellation without
        # cancelling the owner task out from under a just-created process.
        assert chat.cancel_session(sid) is True
        assert turn.commands_starting == 1
        assert not task.done(), "launching helper's owner task was cancelled"
        release_spawn.set()
        # sleep(0.01), not sleep(0): getting here runs session reads that
        # aiosqlite services on a worker thread, and a bare yield only drains
        # callbacks already queued — 20 of those pass in microseconds, before
        # the thread has replied. Overshooting is safe here because FakeCapture
        # blocks in communicate() until the test releases it.
        for _ in range(20):
            if command in runtime.running:
                break
            await asyncio.sleep(0.01)
        assert command in runtime.running, "captured helper was not turn-owned"
        assert command.cancelled, "deferred Esc did not signal helper after launch"
        command.stopped.set()
        with contextlib.suppress(asyncio.CancelledError):
            await task

        assert command not in runtime.running, "finished helper remained registered"
        print("PASS test_captured_helper_is_owned_and_cancelled_with_turn")
    finally:
        command_runner.start_capture = original
        chat_runtime.runtimes.pop(sid, None)


async def test_cancelled_command_aborts_turn_before_agent_can_retry() -> None:
    """A cancelled CLI must skip hooks and finish the owning turn as cancelled."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        db = await Database(str(tmp / "state.db")).connect()
        svc = SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db, sessions=svc))
        session = await svc.create_session(str(cfg), "dev")
        sid = session["id"]
        ws = FakeWS()
        chat.register_conn(sid, ws)

        class SlowRC(FakeRC):
            """A command that is observably mid-run.

            FakeRC separates its lines with ``sleep(0)``, so all 100 drain
            inside a single turn of the loop and the command joins and leaves
            ``running`` almost instantaneously. Whether the poll below caught
            that window came down to how many loop turns the dispatch path
            happened to take, and the assertion failed on roughly half of all
            runs. A real per-line delay keeps the command in ``running`` until
            the test cancels it, which is the state under test.
            """

            async def stream_lines(self):
                for line in self._lines:
                    if self.cancelled:
                        return
                    await asyncio.sleep(0.01)
                    yield line

        command = SlowRC(["waiting"] * 100)
        hook_called = False

        async def fake_start(argv, cwd):  # noqa: ANN001
            return command

        async def forbidden_hook(a, sid_, capture_command=None):  # noqa: ANN001
            nonlocal hook_called
            hook_called = True
            return {"hosts_updated": 0}

        orig_start, orig_check = command_runner.start_command, hook.run_check
        command_runner.start_command, hook.run_check = fake_start, forbidden_hook
        try:
            task = chat.dispatch(app, sid, "/health")
            assert task is not None
            # Real sleep, safe only because SlowRC holds the window open: a
            # plain FakeRC drains before the first poll and this fails 100%.
            for _ in range(20):
                if chat_runtime.runtime(sid).running:
                    break
                await asyncio.sleep(0.01)
            assert chat_runtime.runtime(sid).running, (
                "command never became cancellable"
            )

            assert chat.cancel_session(sid) is True
            with contextlib.suppress(asyncio.CancelledError):
                await task

            assert command.cancelled
            assert not hook_called, "post-command work ran after cancellation"
            assert chat.active_turn(sid) is None
            ends = [event for event in ws.sent if event["kind"] == "agent_end"]
            assert len(ends) == 1, ends
            assert ends[0].get("cancelled") is True
            assert ends[0]["failed"] is False
            print("PASS test_cancelled_command_aborts_turn_before_agent_can_retry")
        finally:
            command_runner.start_command, hook.run_check = orig_start, orig_check
            await chat.cleanup_session(sid)
            await db.close()


async def test_immediate_cancel_still_finishes_the_ui_turn() -> None:
    """Esc can arrive before a newly dispatched task gets its first timeslice."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        db = await Database(str(tmp / "state.db")).connect()
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db))
        sid = "s-immediate-cancel"
        await db.upsert_session({"id": sid})
        ws = FakeWS()
        chat.register_conn(sid, ws)

        async def must_not_run(app_, sid_, content):  # noqa: ANN001
            raise AssertionError("cancelled turn entered message handling")

        orig = chat.handle_message
        chat.handle_message = must_not_run
        try:
            task = chat.dispatch(app, sid, "hello")
            assert task is not None
            assert chat.cancel_session(sid) is True
            with contextlib.suppress(asyncio.CancelledError):
                await task

            ends = [event for event in ws.sent if event["kind"] == "agent_end"]
            assert len(ends) == 1 and ends[0].get("cancelled") is True, ends
            assert chat.active_turn(sid) is None
            print("PASS test_immediate_cancel_still_finishes_the_ui_turn")
        finally:
            chat.handle_message = orig
            await chat.cleanup_session(sid)
            await db.close()


async def test_cleanup_all_force_stops_and_awaits_stubborn_turn() -> None:
    """Shutdown cannot close the DB while an unresponsive turn is still alive."""

    class StubbornCommand(FakeRC):
        def __init__(self) -> None:
            super().__init__([])
            self.force_killed = False

        def force_kill(self) -> None:
            self.force_killed = True

    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        db = await Database(str(tmp / "state.db")).connect()
        sid = "s-shutdown"
        await db.upsert_session({"id": sid})
        app = types.SimpleNamespace(state=types.SimpleNamespace(db=db))
        ws = FakeWS()
        chat.register_conn(sid, ws)
        started = asyncio.Event()

        async def stubborn_turn(app_, sid_, content):  # noqa: ANN001
            started.set()
            await asyncio.Event().wait()

        original = chat.handle_message
        chat.handle_message = stubborn_turn
        command = StubbornCommand()
        try:
            task = chat.dispatch(app, sid, "wait forever")
            assert task is not None
            await started.wait()
            chat_runtime.runtime(sid).running.add(command)

            await chat.cleanup_all(timeout=0.01)

            assert command.cancelled, "shutdown did not request graceful cancellation"
            assert command.force_killed, "shutdown deadline did not force-stop command"
            assert task.done(), "shutdown returned before the turn finished"
            runtime = chat_runtime.runtime(sid)
            assert runtime.turn is None and not runtime.running
            print("PASS test_cleanup_all_force_stops_and_awaits_stubborn_turn")
        finally:
            chat.handle_message = original
            await db.close()


async def _main() -> None:
    await test_direct_dispatch_emits_and_persists()
    await test_start_failure_finishes_turn_and_restores_status()
    test_final_status_precedence()
    await test_replay_matches_live_event_shape()
    await test_resume_reports_in_flight_turn()
    await test_turn_flag_cleared_when_turn_raises()
    await test_reattach_targets_current_conn()
    await test_agent_command_routes_to_agent()
    await test_run_dreadgoad_tool_validates_and_runs()
    await test_direct_command_rejects_extra_args()
    test_instructions_renders_system_prompt()
    await test_health_emits_report_and_suppresses_json()
    await test_swap_model_preserves_thread_and_persists()
    await test_swap_model_no_live_agent()
    await test_dispatch_rejects_queued_turn_and_allows_concurrency()
    await test_cancel_session_cancels_every_parallel_command()
    await test_captured_helper_is_owned_and_cancelled_with_turn()
    await test_cancelled_command_aborts_turn_before_agent_can_retry()
    await test_immediate_cancel_still_finishes_the_ui_turn()
    await test_cleanup_all_force_stops_and_awaits_stubborn_turn()
    await test_cleanup_session_evicts()
    test_cleanup_reservation_is_exclusive()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
