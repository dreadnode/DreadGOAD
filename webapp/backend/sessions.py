"""Session lifecycle service (design §4.2, §4.3, §7).

A session = a ``(config_path, env)`` anchor + a derived snapshot + a working
dir. Create/list/get/delete over the SQLite layer; topology is seeded at
create time (config hosts if the lab exists, infra nodes otherwise).
"""

from __future__ import annotations

import re
import shutil
import typing as t
import uuid
from datetime import datetime, timezone
from pathlib import Path

from . import labconfig
from .db import Database


def _slug(s: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", (s or "").lower()).strip("-")[:40] or "session"


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


class SessionService:
    def __init__(
        self, db: Database, repo_root: str | Path, sessions_root: str | Path
    ) -> None:
        self.db = db
        self.repo_root = str(repo_root)
        self.sessions_root = Path(sessions_root)

    async def create_session(
        self,
        config_path: str,
        env: str,
        model: str | None = None,
        label: str | None = None,
    ) -> dict[str, t.Any]:
        """Attach a session to an existing ``(config_path, env)``."""
        snap = labconfig.derive_snapshot(config_path, env)

        sid = "s-" + uuid.uuid4().hex[:8]
        lbl = (
            label or f"{env} · {snap.get('provider')}/{snap.get('variant_name') or env}"
        )
        dirname = f"{_slug(label or env)}-{sid[2:]}"
        sdir = self.sessions_root / dirname
        sdir.mkdir(parents=True, exist_ok=True)

        session = {
            "id": sid,
            "label": lbl,
            "model": model,
            "status": "new",
            "anchor": {"config_path": str(config_path), "env": env},
            "snapshot": snap,
            "session_dir": str(sdir),
            "created_at": _now(),
            "updated_at": _now(),
        }
        await self.db.upsert_session(session)

        topo = self._seed_topology(snap)
        topo["session_id"] = sid
        await self.db.upsert_range(sid, topo)

        await self.db.append_event(sid, "session_created", {"label": lbl})
        return session

    async def create_new_env_session(
        self,
        config_path: str,
        env_name: str,
        env_fields: dict[str, t.Any],
        top_level: dict[str, t.Any] | None = None,
        model: str | None = None,
        label: str | None = None,
    ) -> dict[str, t.Any]:
        """Create-new-env flow: write the env into the yaml, then attach."""
        labconfig.write_new_env(config_path, env_name, env_fields, top_level)
        return await self.create_session(
            config_path, env_name, model=model, label=label
        )

    def _seed_topology(self, snapshot: dict[str, t.Any]) -> dict[str, t.Any]:
        cfg = labconfig.lab_config_path(self.repo_root, snapshot.get("lab"))
        return labconfig.seed_topology(cfg, snapshot.get("provider"))

    async def list_sessions(self) -> list[dict[str, t.Any]]:
        return await self.db.list_sessions()

    async def get_session(self, session_id: str) -> dict[str, t.Any] | None:
        return await self.db.get_session(session_id)

    async def delete_session(self, session_id: str) -> bool:
        """Delete a session, its range/events, and its working dir."""
        session = await self.db.get_session(session_id)
        if session is None:
            return False
        await self.db.delete_session(session_id)
        sdir = session.get("session_dir")
        if sdir:
            shutil.rmtree(sdir, ignore_errors=True)
        return True

    async def set_status(self, session_id: str, status: str) -> None:
        """Flush a status-critical write immediately (§6.1 durability)."""
        session = await self.db.get_session(session_id)
        if session is None:
            return
        session["status"] = status
        session["updated_at"] = _now()
        await self.db.upsert_session(session)
