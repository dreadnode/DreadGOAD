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

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))
os.environ["DREADGOAD_WEBAPP_STATE_ROOT"] = tempfile.mkdtemp(prefix="dg-chat-")

from webapp.backend import chat, hook  # noqa: E402
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
    await test_dispatch_serialization_and_concurrency()
    await test_cleanup_session_evicts()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
