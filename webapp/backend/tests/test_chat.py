"""Tests for the chat runtime (Phase 3: T3.1) — the F1/F2/F3 fixes.

Covers: direct-dispatch flow (emit sequence + event persistence), per-session
serialization vs cross-session concurrency, and cleanup_session eviction.

Standalone:  python webapp/backend/tests/test_chat.py
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import sys
import tempfile
import types
from contextlib import asynccontextmanager

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))
os.environ["DREADGOAD_WEBAPP_STATE_ROOT"] = tempfile.mkdtemp(prefix="dg-chat-")

from webapp.backend import agent, chat, hook  # noqa: E402
from webapp.backend.db import Database  # noqa: E402
from webapp.backend.sessions import SessionService  # noqa: E402

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

        async def fake_check(a, sid_):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = chat.start_command, hook.run_check
        chat.start_command = fake_start
        hook.run_check = fake_check
        try:
            s = await svc.create_session(str(cfg), "dev")
            ws = FakeWS()
            chat.register_conn(s["id"], ws)
            await chat.handle_message(app, s["id"], "/instances")

            kinds = [m["kind"] for m in ws.sent]
            assert kinds[0] == "user_message", kinds
            assert kinds[-1] == "agent_end", kinds
            assert "command_progress" in kinds, kinds
            assert "check_run" in kinds, kinds

            # command_progress is live-only and must NOT be persisted (§5.4).
            ekinds = [e["kind"] for e in await db.get_events(s["id"])]
            assert "user_message" in ekinds and "agent_end" in ekinds, ekinds
            assert "command_progress" not in ekinds, "progress must not persist"
            print("PASS test_direct_dispatch_emits_and_persists")
        finally:
            chat.start_command, hook.run_check = orig_start, orig_check
            await db.close()


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

        async def fake_check(a, sid_):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = chat.start_command, hook.run_check
        chat.start_command, hook.run_check = fake_start, fake_check
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
            chat.start_command, hook.run_check = orig_start, orig_check
            await db.close()


async def test_dispatch_serialization_and_concurrency() -> None:
    """Same session runs serially; different sessions overlap (§6.4/§7)."""
    order: list[str] = []

    async def fake_handle(app, sid, content):  # noqa: ANN001
        order.append(f"start:{sid}")
        await asyncio.sleep(0.02)
        order.append(f"end:{sid}")

    orig = chat.handle_message
    chat.handle_message = fake_handle
    try:
        # same session → strictly serial (no interleave)
        await asyncio.gather(
            chat.dispatch(None, "A", "1"),
            chat.dispatch(None, "A", "2"),
        )
        assert order == ["start:A", "end:A", "start:A", "end:A"], order

        # different sessions → overlap
        order.clear()
        await asyncio.gather(
            chat.dispatch(None, "X", "1"),
            chat.dispatch(None, "Y", "1"),
        )
        assert order[:2] in (["start:X", "start:Y"], ["start:Y", "start:X"]), order
        print("PASS test_dispatch_serialization_and_concurrency")
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

    # allowed command → routed through run_cli
    out = await tool.fn(command="/up", args=["--from", "x"])
    assert calls == [("/up", ["--from", "x"])], calls
    assert "succeeded" in out, out

    # disallowed (operator-only) command → refused, run_cli NOT called
    calls.clear()
    refused = await tool.fn(command="/destroy", args=[])
    assert "Refused" in refused, refused
    assert calls == [], "run_cli must not run for a disallowed command"
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

        report = json.dumps(
            {
                "passed": 1,
                "failed": 1,
                "skipped": 0,
                "checks": [
                    {
                        "name": "DC01 DC",
                        "host": "DC01",
                        "status": "OK",
                        "detail": "DC01",
                    },
                    {
                        "name": "DC02 Trust",
                        "host": "DC02",
                        "status": "FAIL",
                        "detail": "no trust",
                    },
                ],
            }
        )

        # requireInfra prints a "credentials OK" line to stdout before the JSON;
        # the report must still be extracted from the noisy, merged output.
        noisy = "  aws credentials OK (arn:aws:iam::1:user/x)\n" + report

        async def fake_start(argv, cwd):  # noqa: ANN001
            return FakeRC(noisy.splitlines(), rc=1)  # exit 1: a check failed

        async def fake_check(a, sid_):  # noqa: ANN001
            return {"hosts_updated": 0}

        orig_start, orig_check = chat.start_command, hook.run_check
        chat.start_command, hook.run_check = fake_start, fake_check
        try:
            s = await svc.create_session(str(cfg), "dev")
            ws = FakeWS()
            chat.register_conn(s["id"], ws)
            await chat.handle_message(app, s["id"], "/health")

            kinds = [m["kind"] for m in ws.sent]
            assert "health_report" in kinds, kinds
            assert "command_progress" not in kinds, (
                "raw JSON must be suppressed for /health"
            )
            hr = next(m for m in ws.sent if m["kind"] == "health_report")
            assert hr["passed"] == 1 and hr["failed"] == 1, hr
            assert len(hr["checks"]) == 2, hr

            # A failing /health must NOT mark the session errored (§ status fix).
            sess = await db.get_session(s["id"])
            assert sess is not None
            assert sess["status"] == "running", sess["status"]
            print("PASS test_health_emits_report_and_suppresses_json")
        finally:
            chat.start_command, hook.run_check = orig_start, orig_check
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
        chat._agents[s["id"]] = old
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
            new = chat._agents[s["id"]]
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
            chat._agents.pop(s["id"], None)
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
        chat._agents.pop(s["id"], None)  # ensure not cached

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
            assert s["id"] not in chat._agents
            sess = await db.get_session(s["id"])
            assert sess is not None and sess["model"] == "m2", sess
            # unknown session → None
            assert await chat.swap_model(app, "ghost", "m3") is None
            print("PASS test_swap_model_no_live_agent")
        finally:
            chat.create_agent = orig
            await db.close()


async def test_cleanup_session_evicts() -> None:
    chat._agents["z"] = object()
    chat._locks["z"] = asyncio.Lock()
    chat._running["z"] = FakeRC([])
    chat.cleanup_session("z")
    assert "z" not in chat._agents, "agent not evicted"
    assert "z" not in chat._locks, "lock not evicted"
    assert "z" not in chat._running, "running not evicted"
    print("PASS test_cleanup_session_evicts")


async def _main() -> None:
    await test_direct_dispatch_emits_and_persists()
    await test_reattach_targets_current_conn()
    await test_agent_command_routes_to_agent()
    await test_run_dreadgoad_tool_validates_and_runs()
    await test_direct_command_rejects_extra_args()
    test_instructions_renders_system_prompt()
    await test_health_emits_report_and_suppresses_json()
    await test_swap_model_preserves_thread_and_persists()
    await test_swap_model_no_live_agent()
    await test_dispatch_serialization_and_concurrency()
    await test_cleanup_session_evicts()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
