"""SQLite persistence layer (design §6).

Document model over SQLite: each collection is a table whose payload is a JSON
column; only queried fields (event `session_id`/`seq`/`kind`) are real columns.

Concurrency & safety: all DB work runs on a **single-worker thread executor**,
so operations serialize naturally (no lost updates) and the connection is only
ever touched from its own thread. This is the "one serialized writer" from §6.1
without an extra dependency (stdlib ``sqlite3`` only). Note: because every op
(reads included) goes through the one worker, there is no read/write
concurrency — WAL here buys durable commits, not concurrent readers.
"""

from __future__ import annotations

import asyncio
import json
import sqlite3
import typing as t
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone

_SCHEMA = """
CREATE TABLE IF NOT EXISTS sessions (
    id   TEXT PRIMARY KEY,
    data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ranges (
    session_id TEXT PRIMARY KEY,
    data       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
    session_id TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    kind       TEXT NOT NULL,
    ts         TEXT NOT NULL,
    payload    TEXT NOT NULL,
    PRIMARY KEY (session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_events_kind ON events (session_id, kind);
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
"""


def _utcnow() -> str:
    return datetime.now(timezone.utc).isoformat()


class Database:
    """Async wrapper over a single-threaded SQLite connection."""

    def __init__(self, path: str) -> None:
        self._path = path
        self._executor = ThreadPoolExecutor(max_workers=1, thread_name_prefix="dg-db")
        self._conn: sqlite3.Connection | None = None

    # --- lifecycle ---------------------------------------------------------

    async def connect(self) -> "Database":
        """Open the connection and apply the schema. Returns self for chaining.

        On failure the worker thread is shut down rather than leaked.
        """
        try:
            await self._run(self._connect)
        except BaseException:
            # Don't leak the worker thread if connection setup fails.
            self._executor.shutdown(wait=False)
            raise
        return self

    def _connect(self) -> None:
        conn = sqlite3.connect(self._path)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL")
        # NORMAL: durable across an app crash; a power/OS crash may lose only the
        # last commit. Fine for this local tool; use FULL for strict durability.
        conn.execute("PRAGMA synchronous=NORMAL")
        conn.execute("PRAGMA foreign_keys=ON")
        conn.executescript(_SCHEMA)
        conn.commit()
        self._conn = conn

    async def close(self) -> None:
        """Close the connection and join the worker thread."""
        await self._run(self._close)
        self._executor.shutdown(wait=True)

    def _close(self) -> None:
        if self._conn is not None:
            self._conn.close()
            self._conn = None

    async def _run(self, fn: t.Callable[..., t.Any], *args: t.Any) -> t.Any:
        loop = asyncio.get_running_loop()
        return await loop.run_in_executor(self._executor, fn, *args)

    @property
    def _c(self) -> sqlite3.Connection:
        if self._conn is None:
            raise RuntimeError("Database not connected; call connect() first")
        return self._conn

    # --- sessions ----------------------------------------------------------

    async def upsert_session(self, session: dict[str, t.Any]) -> None:
        """Insert or replace a session document, keyed by its ``id``."""
        await self._run(self._upsert_session, session)

    def _upsert_session(self, session: dict[str, t.Any]) -> None:
        sid = session["id"]
        self._c.execute(
            "INSERT INTO sessions (id, data) VALUES (?, ?) "
            "ON CONFLICT(id) DO UPDATE SET data=excluded.data",
            (sid, json.dumps(session)),
        )
        self._c.commit()

    async def get_session(self, session_id: str) -> dict[str, t.Any] | None:
        """Return the session document, or None if it doesn't exist."""
        return await self._run(self._get_session, session_id)

    def _get_session(self, session_id: str) -> dict[str, t.Any] | None:
        row = self._c.execute(
            "SELECT data FROM sessions WHERE id=?", (session_id,)
        ).fetchone()
        return json.loads(row["data"]) if row else None

    async def list_sessions(self) -> list[dict[str, t.Any]]:
        """Return every session document (unordered)."""
        return await self._run(self._list_sessions)

    def _list_sessions(self) -> list[dict[str, t.Any]]:
        rows = self._c.execute("SELECT data FROM sessions").fetchall()
        return [json.loads(r["data"]) for r in rows]

    async def delete_session(self, session_id: str) -> None:
        """Delete a session and cascade to its range doc and event log."""
        await self._run(self._delete_session, session_id)

    def _delete_session(self, session_id: str) -> None:
        self._c.execute("DELETE FROM sessions WHERE id=?", (session_id,))
        self._c.execute("DELETE FROM ranges WHERE session_id=?", (session_id,))
        self._c.execute("DELETE FROM events WHERE session_id=?", (session_id,))
        self._c.execute("DELETE FROM meta WHERE key=?", (f"thread:{session_id}",))
        self._c.commit()

    # --- ranges ------------------------------------------------------------

    async def upsert_range(self, session_id: str, rng: dict[str, t.Any]) -> None:
        """Insert or replace a session's range topology document.

        Range discovery/health writers often hold a snapshot across network
        I/O. If a layout save landed meanwhile, keep that newer layout instead
        of letting the stale whole-document write restore old coordinates.
        """
        await self._run(self._upsert_range, session_id, rng)

    def _upsert_range(self, session_id: str, rng: dict[str, t.Any]) -> None:
        document = dict(rng)
        incoming_revision = self._range_layout_revision(document)
        row = self._c.execute(
            "SELECT data FROM ranges WHERE session_id=?", (session_id,)
        ).fetchone()
        if row is not None:
            current = json.loads(row["data"])
            current_revision = self._range_layout_revision(current)
            if current_revision > incoming_revision:
                document["layout"] = current.get("layout", {})
                incoming_revision = current_revision
        document["layout_revision"] = incoming_revision
        document.setdefault("layout", {})
        self._c.execute(
            "INSERT INTO ranges (session_id, data) VALUES (?, ?) "
            "ON CONFLICT(session_id) DO UPDATE SET data=excluded.data",
            (session_id, json.dumps(document)),
        )
        self._c.commit()

    @staticmethod
    def _range_layout_revision(rng: dict[str, t.Any]) -> int:
        revision = rng.get("layout_revision", 0)
        return (
            revision
            if isinstance(revision, int)
            and not isinstance(revision, bool)
            and revision >= 0
            else 0
        )

    async def update_range_layout(
        self,
        session_id: str,
        layout: dict[str, dict[str, int]],
        expected_revision: int,
    ) -> tuple[bool, int] | None:
        """Atomically replace layout when ``expected_revision`` is current.

        ``None`` means the range does not exist. Otherwise the boolean reports
        whether the write succeeded and the integer is the current/new layout
        revision. The complete read-check-write runs as one database-worker job,
        so range status updates cannot interleave inside it.
        """
        return await self._run(
            self._update_range_layout, session_id, layout, expected_revision
        )

    def _update_range_layout(
        self,
        session_id: str,
        layout: dict[str, dict[str, int]],
        expected_revision: int,
    ) -> tuple[bool, int] | None:
        row = self._c.execute(
            "SELECT data FROM ranges WHERE session_id=?", (session_id,)
        ).fetchone()
        if row is None:
            return None

        rng = json.loads(row["data"])
        current_revision = self._range_layout_revision(rng)
        if expected_revision != current_revision:
            return False, current_revision

        new_revision = current_revision + 1
        rng["layout"] = layout
        rng["layout_revision"] = new_revision
        self._c.execute(
            "UPDATE ranges SET data=? WHERE session_id=?",
            (json.dumps(rng), session_id),
        )
        self._c.commit()
        return True, new_revision

    async def get_range(self, session_id: str) -> dict[str, t.Any] | None:
        """Return a session's range document, or None if it doesn't exist."""
        return await self._run(self._get_range, session_id)

    def _get_range(self, session_id: str) -> dict[str, t.Any] | None:
        row = self._c.execute(
            "SELECT data FROM ranges WHERE session_id=?", (session_id,)
        ).fetchone()
        if row is None:
            return None
        rng = json.loads(row["data"])
        rng.setdefault("layout", {})
        rng["layout_revision"] = self._range_layout_revision(rng)
        return rng

    # --- events ------------------------------------------------------------

    MAX_EVENTS = 2000

    async def prune_events(self, session_id: str, keep: int = MAX_EVENTS) -> int:
        """Delete the oldest events for a session, keeping the last ``keep``."""
        return await self._run(self._prune_events, session_id, keep)

    def _prune_events(self, session_id: str, keep: int) -> int:
        row = self._c.execute(
            "SELECT seq FROM events WHERE session_id=? "
            "ORDER BY seq DESC LIMIT 1 OFFSET ?",
            (session_id, keep - 1),
        ).fetchone()
        if row is None:
            return 0
        cursor = self._c.execute(
            "DELETE FROM events WHERE session_id=? AND seq < ?",
            (session_id, row["seq"]),
        )
        self._c.commit()
        return cursor.rowcount

    async def append_event(
        self, session_id: str, kind: str, payload: dict[str, t.Any]
    ) -> int:
        """Append an event, assigning a monotonic per-session ``seq``.

        Returns the assigned seq. Runs on the single DB thread, so the
        read-then-insert is atomic with respect to other DB operations.
        """
        return await self._run(self._append_event, session_id, kind, payload)

    def _append_event(
        self, session_id: str, kind: str, payload: dict[str, t.Any]
    ) -> int:
        exists = self._c.execute(
            "SELECT 1 FROM sessions WHERE id=?", (session_id,)
        ).fetchone()
        if exists is None:
            raise LookupError(f"session not found: {session_id}")
        row = self._c.execute(
            "SELECT COALESCE(MAX(seq), 0) + 1 AS next FROM events WHERE session_id=?",
            (session_id,),
        ).fetchone()
        seq = int(row["next"])
        self._c.execute(
            "INSERT INTO events (session_id, seq, kind, ts, payload) VALUES (?, ?, ?, ?, ?)",
            (session_id, seq, kind, _utcnow(), json.dumps(payload)),
        )
        self._c.commit()
        return seq

    async def get_events(
        self, session_id: str, kinds: t.Sequence[str] | None = None
    ) -> list[dict[str, t.Any]]:
        """Return events for a session ordered by ``seq``.

        ``kinds=None`` returns all events; a non-empty sequence filters to those
        kinds (e.g. chat-kinds for replay); an **empty** sequence returns no
        events. Each returned dict is ``{seq, kind, ts, payload}``.
        """
        return await self._run(self._get_events, session_id, kinds)

    def _get_events(
        self, session_id: str, kinds: t.Sequence[str] | None
    ) -> list[dict[str, t.Any]]:
        if kinds is not None:
            if not kinds:
                return []  # empty filter → no events (None means "all")
            placeholders = ",".join("?" for _ in kinds)
            sql = (
                f"SELECT seq, kind, ts, payload FROM events "
                f"WHERE session_id=? AND kind IN ({placeholders}) ORDER BY seq"
            )
            rows = self._c.execute(sql, (session_id, *kinds)).fetchall()
        else:
            rows = self._c.execute(
                "SELECT seq, kind, ts, payload FROM events WHERE session_id=? ORDER BY seq",
                (session_id,),
            ).fetchall()
        return [
            {
                "seq": r["seq"],
                "kind": r["kind"],
                "ts": r["ts"],
                "payload": json.loads(r["payload"]),
            }
            for r in rows
        ]

    # --- meta --------------------------------------------------------------

    async def set_meta(self, key: str, value: t.Any) -> None:
        """Store a JSON-able value in the key/value meta table (e.g. schema version)."""
        await self._run(self._set_meta, key, value)

    def _set_meta(self, key: str, value: t.Any) -> None:
        self._c.execute(
            "INSERT INTO meta (key, value) VALUES (?, ?) "
            "ON CONFLICT(key) DO UPDATE SET value=excluded.value",
            (key, json.dumps(value)),
        )
        self._c.commit()

    async def get_meta(self, key: str) -> t.Any | None:
        """Return a meta value, or None if the key is unset."""
        return await self._run(self._get_meta, key)

    def _get_meta(self, key: str) -> t.Any | None:
        row = self._c.execute("SELECT value FROM meta WHERE key=?", (key,)).fetchone()
        return json.loads(row["value"]) if row else None
