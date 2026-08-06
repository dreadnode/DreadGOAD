"""Tests for the SQLite layer (Phase 1: T1.1 + T1.2).

Runnable two ways:
  - standalone:  python console/backend/tests/test_db.py   (no pytest needed)
  - pytest:      (with pytest-asyncio installed)
"""

from __future__ import annotations

import asyncio
import pathlib
import sys
import tempfile

# Make `console.backend.db` importable when run as a standalone script.
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.db import Database  # noqa: E402


async def _fresh_db() -> tuple[Database, str]:
    tmp = tempfile.NamedTemporaryFile(suffix=".db", delete=False)
    tmp.close()
    db = await Database(tmp.name).connect()
    return db, tmp.name


async def test_session_and_range_crud() -> None:
    db, _ = await _fresh_db()
    try:
        s = {"id": "s-1", "label": "test", "status": "new"}
        await db.upsert_session(s)
        assert await db.get_session("s-1") == s, "session round-trip failed"

        # update
        s["status"] = "running"
        await db.upsert_session(s)
        updated = await db.get_session("s-1")
        assert updated is not None and updated["status"] == "running", "update failed"

        assert len(await db.list_sessions()) == 1, "list count wrong"

        rng = {
            "session_id": "s-1",
            "hosts": [{"id": "dc01"}],
            "edges": [],
            "layout": {},
        }
        await db.upsert_range("s-1", rng)
        got_range = await db.get_range("s-1")
        assert got_range is not None and got_range["hosts"][0]["id"] == "dc01", (
            "range round-trip failed"
        )

        # cascade delete
        await db.delete_session("s-1")
        assert await db.get_session("s-1") is None, "session not deleted"
        assert await db.get_range("s-1") is None, "range not cascade-deleted"
        print("PASS test_session_and_range_crud")
    finally:
        await db.close()


async def test_event_seq_and_replay() -> None:
    db, _ = await _fresh_db()
    try:
        await db.append_event("s-1", "user_message", {"content": "hi"})
        await db.append_event("s-1", "generation", {"content": "hello", "usage": {}})
        await db.append_event("s-1", "check_run", {"hosts_updated": 3})
        # different session has its own seq space
        await db.append_event("s-2", "user_message", {"content": "other"})

        evts = await db.get_events("s-1")
        seqs = [e["seq"] for e in evts]
        assert seqs == [1, 2, 3], f"seq not monotonic 1..3: {seqs}"

        # per-session isolation
        assert [e["seq"] for e in await db.get_events("s-2")] == [1], (
            "cross-session seq leak"
        )

        # kind filter (chat-replay style) excludes system kinds, preserves order
        chat = await db.get_events("s-1", kinds=["user_message", "generation"])
        assert [e["kind"] for e in chat] == ["user_message", "generation"], (
            "kind filter wrong"
        )
        assert [e["seq"] for e in chat] == [1, 2], "kind filter broke ordering"

        # payload preserved
        assert chat[0]["payload"]["content"] == "hi", "payload not preserved"

        # None → all; empty list → nothing (not "all")
        assert len(await db.get_events("s-1", kinds=None)) == 3, (
            "None should return all"
        )
        assert await db.get_events("s-1", kinds=[]) == [], (
            "empty kinds must return nothing"
        )
        print("PASS test_event_seq_and_replay")
    finally:
        await db.close()


async def test_meta_round_trip() -> None:
    db, _ = await _fresh_db()
    try:
        assert await db.get_meta("schema_version") is None, (
            "missing meta should be None"
        )
        await db.set_meta("schema_version", 1)
        assert await db.get_meta("schema_version") == 1, "meta round-trip failed"
        await db.set_meta("schema_version", 2)  # upsert
        assert await db.get_meta("schema_version") == 2, "meta upsert failed"
        # JSON values (not just scalars) round-trip
        await db.set_meta("obj", {"a": [1, 2]})
        assert await db.get_meta("obj") == {"a": [1, 2]}, "meta JSON value failed"
        print("PASS test_meta_round_trip")
    finally:
        await db.close()


async def test_concurrent_writes_no_loss() -> None:
    """N interleaved async writes must all persist with unique, gapless seqs."""
    db, _ = await _fresh_db()
    try:
        n = 100
        await asyncio.gather(
            *[db.append_event("s-1", "generation", {"i": i}) for i in range(n)]
        )
        evts = await db.get_events("s-1")
        seqs = sorted(e["seq"] for e in evts)
        assert len(evts) == n, f"lost writes: got {len(evts)} of {n}"
        assert seqs == list(range(1, n + 1)), (
            "seqs not unique/gapless under concurrency"
        )

        # concurrent session upserts with distinct ids
        await asyncio.gather(
            *[db.upsert_session({"id": f"s-{i}", "status": "new"}) for i in range(n)]
        )
        assert len(await db.list_sessions()) == n, (
            "concurrent session upserts lost rows"
        )
        print("PASS test_concurrent_writes_no_loss")
    finally:
        await db.close()


async def test_layout_revision_protects_against_stale_range_writes() -> None:
    """A stale health/topology snapshot cannot restore an older layout."""
    db, _ = await _fresh_db()
    try:
        original = {
            "session_id": "s-layout",
            "hosts": [{"id": "dc01", "status": "pending"}],
            "edges": [],
            "layout": {"dc01": {"x": 1, "y": 2}},
        }
        await db.upsert_range("s-layout", original)
        stale = await db.get_range("s-layout")
        assert stale is not None and stale["layout_revision"] == 0, stale

        # A normal range update lands first; the atomic layout update must keep
        # every non-layout field from that latest document.
        current = await db.get_range("s-layout")
        assert current is not None
        current["hosts"][0]["status"] = "running"
        await db.upsert_range("s-layout", current)
        saved = await db.update_range_layout(
            "s-layout", {"dc01": {"x": 100, "y": 200}}, 0
        )
        assert saved == (True, 1), saved
        after_layout = await db.get_range("s-layout")
        assert after_layout is not None
        assert after_layout["hosts"][0]["status"] == "running", after_layout
        assert after_layout["layout"]["dc01"] == {"x": 100, "y": 200}

        # A writer that read revision 0 before the drag may still update range
        # state, but its old coordinates must be replaced by revision 1.
        stale["hosts"][0]["status"] = "stopped"
        await db.upsert_range("s-layout", stale)
        after_stale_writer = await db.get_range("s-layout")
        assert after_stale_writer is not None
        assert after_stale_writer["layout_revision"] == 1, after_stale_writer
        assert after_stale_writer["layout"]["dc01"] == {"x": 100, "y": 200}

        # A stale browser save is refused rather than overwriting revision 1.
        rejected = await db.update_range_layout(
            "s-layout", {"dc01": {"x": 3, "y": 4}}, 0
        )
        assert rejected == (False, 1), rejected
        assert await db.update_range_layout("missing", {}, 0) is None
        print("PASS test_layout_revision_protects_against_stale_range_writes")
    finally:
        await db.close()


async def _main() -> None:
    await test_session_and_range_crud()
    await test_event_seq_and_replay()
    await test_meta_round_trip()
    await test_concurrent_writes_no_loss()
    await test_layout_revision_protects_against_stale_range_writes()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
