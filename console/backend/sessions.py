"""Session lifecycle service (design §4.2, §4.3, §7).

A session = a ``(config_path, env)`` anchor + a derived snapshot + a working
dir. Create/list/get/delete over the SQLite layer; topology is seeded at
create time (config hosts if the lab exists, infra nodes otherwise).
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import re
import shutil
import typing as t
import uuid
from datetime import datetime, timezone
from pathlib import Path

from . import configstore, labconfig, scaffold
from .db import Database


def _slug(s: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", (s or "").lower()).strip("-")[:40] or "session"


def _now() -> str:
    return datetime.now(timezone.utc).isoformat()


def default_label(config_path: str, env: str, snapshot: dict[str, t.Any]) -> str:
    """The tab name for a session, as ``<config>/<env>``.

    A tab only has to answer *which session is this*; what it is — provider,
    region, resource group, account — is already on screen in the range header
    (RangeView's fields). The previous format spent its width the other way
    round, on ``<env> · <provider>/<variant name>``: it repeated the provider
    the header was showing, omitted the config entirely, and ended in the
    variant name, which defaults to the environment name and so restated it.

    The config is the axis that actually distinguishes sessions now that more
    than one can exist, and it was the only thing not shown at all.

    The variant is appended only when it differs from the environment name,
    which is the sole case where that third segment carries information.

    There is no rename endpoint, so this is the name a session keeps for life.
    """
    stem = Path(config_path).stem or "config"
    name = f"{stem}/{env}"
    variant_name = snapshot.get("variant_name")
    if variant_name and variant_name != env:
        name = f"{name} · {variant_name}"
    return name


class SessionService:
    """Create, read, and delete sessions over the SQLite layer.

    Owns the session's derived snapshot, its working directory, and the initial
    range topology seeded from the lab config at creation time.
    """

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
        lbl = label or default_label(config_path, env, snap)
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
        session = await self.create_session(
            config_path, env_name, model=model, label=label
        )
        await self._scaffold_for(session, env_fields)
        return session

    async def _scaffold_for(
        self, session: dict[str, t.Any], env_fields: dict[str, t.Any]
    ) -> None:
        """Build the environment's infrastructure and record what happened.

        Failure is deliberately NOT fatal to session creation. The session and
        its config entry are already valid and useful — the operator can inspect
        them, fix whatever the scaffold complained about, and retry — whereas
        unwinding a successful create because a later step failed would throw
        away the part that worked. The outcome is recorded as an event so it is
        in the chat history rather than only in a response body nobody reads.
        """
        snapshot = session.get("snapshot") or {}
        region = snapshot.get("region")
        anchor = session["anchor"]
        if not region:
            await self.db.append_event(
                session["id"],
                "status",
                {
                    "content": (
                        "Skipped infrastructure scaffolding: no region is set for "
                        "this environment, and env create requires one."
                    )
                },
            )
            return

        ok, output = await scaffold.scaffold_env(
            str(anchor["config_path"]),
            str(anchor["env"]),
            str(region),
            variant=bool(env_fields.get("variant")),
            variant_source=env_fields.get("variant_source"),
            variant_target=env_fields.get("variant_target"),
            vpc_cidr=env_fields.get("vpc_cidr"),
            provider=str(snapshot.get("provider") or ""),
        )
        await self.db.append_event(
            session["id"],
            "status" if ok else "error",
            {
                "content": (
                    f"Infrastructure scaffolded for {anchor['env']}.\n{output}"
                    if ok
                    else (
                        f"Infrastructure was NOT scaffolded for {anchor['env']}, so "
                        f"/up will fail until it is.\n{output}"
                    )
                )
            },
        )

    async def create_config_session(
        self,
        config_name: str,
        provider: str,
        env_name: str,
        env_fields: dict[str, t.Any],
        region: str | None = None,
        model: str | None = None,
        label: str | None = None,
    ) -> dict[str, t.Any]:
        """Create-new-config flow: write a config, put one env in it, attach.

        The whole point of doing this in one call rather than exposing a
        separate config endpoint is that a config with no environment in it is
        not a thing anyone wants — it cannot be attached to, so it would only
        ever be a half-finished state for the UI to explain.

        Unlike :meth:`create_new_env_session`, a failure after the write is
        rolled back. The file is one this call exclusively created (create_config
        opens O_EXCL), so deleting it destroys nothing that existed before — and
        leaving it would be worse than untidy: create_config refuses to overwrite,
        so retrying with the same name would then fail with "already exists" for
        a config the operator never successfully made.
        """
        path = str(configstore.path_for(config_name))
        labconfig.create_config(path, provider, env_name, env_fields, region=region)
        try:
            session = await self.create_session(
                path, env_name, model=model, label=label
            )
        except Exception:
            with contextlib.suppress(OSError):
                os.unlink(path)
            raise
        await self._scaffold_for(session, env_fields)
        return session

    def _seed_topology(self, snapshot: dict[str, t.Any]) -> dict[str, t.Any]:
        cfg = labconfig.lab_config_path(self.repo_root, snapshot.get("lab"))
        return labconfig.seed_topology(cfg, snapshot.get("provider"))

    async def list_sessions(self) -> list[dict[str, t.Any]]:
        """Return every known session."""
        return await self.db.list_sessions()

    async def get_session(self, session_id: str) -> dict[str, t.Any] | None:
        """Return one session, or None if it doesn't exist."""
        return await self.db.get_session(session_id)

    async def delete_session(self, session_id: str) -> bool:
        """Delete a session, its range/events, and its working dir."""
        session = await self.db.get_session(session_id)
        if session is None:
            return False
        sdir = session.get("session_dir")
        if sdir:
            root = self.sessions_root.resolve()
            target = Path(sdir).resolve()
            if target == root or root not in target.parents:
                raise ValueError(
                    "refusing to delete a session dir outside sessions root"
                )
            if target.exists():
                await asyncio.to_thread(shutil.rmtree, target)
        await self.db.delete_session(session_id)
        return True

    async def set_status(self, session_id: str, status: str) -> None:
        """Flush a status-critical write immediately (§6.1 durability)."""
        session = await self.db.get_session(session_id)
        if session is None:
            return
        session["status"] = status
        session["updated_at"] = _now()
        await self.db.upsert_session(session)
