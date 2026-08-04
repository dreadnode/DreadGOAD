"""FastAPI backend entry point.

Phase 0: health/config + static mount.
Phase 2: SQLite-backed session lifecycle REST + RangeView reads.
Later: multiplexed chat + range-state WebSockets, the agent, ingestion hook.
"""

from __future__ import annotations

import json
import os
import typing as t
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.staticfiles import StaticFiles

from . import __version__ as VERSION
from . import chat, paths
from .db import Database
from .sessions import SessionService

# Default model + provider (design decision: Sonnet 5 via OpenRouter).
_DEFAULT_MODEL = os.environ.get(
    "DREADGOAD_WEBAPP_MODEL", "openrouter/anthropic/claude-sonnet-5"
)


@asynccontextmanager
async def _lifespan(app: FastAPI) -> t.AsyncIterator[None]:
    """Open the DB and build the session service on startup."""
    db = await Database(paths.resolve_db_path()).connect()
    await db.set_meta("schema_version", 1)
    app.state.db = db
    app.state.sessions = SessionService(
        db, repo_root=str(paths.repo_root()), sessions_root=paths.sessions_root()
    )
    # Survival: a session left mid-deploy by a crash/restart is reconciled to
    # `interrupted` so the operator knows to re-run (§5.4).
    for s in await db.list_sessions():
        if s.get("status") == "provisioning":
            s["status"] = "interrupted"
            await db.upsert_session(s)
    try:
        yield
    finally:
        await db.close()


app = FastAPI(title="DreadGOAD Web App", lifespan=_lifespan)


# --- health / config -------------------------------------------------------


@app.get("/api/health")
async def health() -> dict[str, t.Any]:
    return {"status": "ok", "version": VERSION}


@app.get("/api/config")
async def get_config() -> dict[str, t.Any]:
    return {
        "version": VERSION,
        "default_model": _DEFAULT_MODEL,
        "default_config_path": str(paths.repo_root() / "dreadgoad.yaml"),
    }


# --- session lifecycle (§7) ------------------------------------------------


def _svc(app: FastAPI) -> SessionService:
    return app.state.sessions


@app.post("/api/sessions")
async def create_session(body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Create a session. Modes: ``attach`` (default) or ``new`` (write env)."""
    svc = _svc(app)
    mode = body.get("mode", "attach")
    config_path = body.get("config_path") or str(paths.repo_root() / "dreadgoad.yaml")
    env = (body.get("env") or "").strip()
    if not env:
        raise HTTPException(status_code=400, detail="env is required")
    model = body.get("model") or _DEFAULT_MODEL
    label = body.get("label")

    if mode == "new":
        env_fields = body.get("env_fields") or {}
        top_level = body.get("top_level")
        return await svc.create_new_env_session(
            config_path, env, env_fields, top_level=top_level, model=model, label=label
        )
    return await svc.create_session(config_path, env, model=model, label=label)


@app.get("/api/sessions")
async def list_sessions() -> dict[str, t.Any]:
    return {"sessions": await _svc(app).list_sessions()}


@app.get("/api/sessions/{session_id}")
async def get_session(session_id: str) -> dict[str, t.Any]:
    s = await _svc(app).get_session(session_id)
    if s is None:
        raise HTTPException(status_code=404, detail="session not found")
    return s


@app.delete("/api/sessions/{session_id}")
async def delete_session(session_id: str) -> dict[str, t.Any]:
    ok = await _svc(app).delete_session(session_id)
    if not ok:
        raise HTTPException(status_code=404, detail="session not found")
    return {"deleted": session_id}


# --- RangeView reads (§7) --------------------------------------------------


@app.get("/api/ranges/{session_id}")
async def get_range(session_id: str) -> dict[str, t.Any]:
    rng = await app.state.db.get_range(session_id)
    if rng is None:
        raise HTTPException(status_code=404, detail="range not found")
    return rng


@app.put("/api/ranges/{session_id}/layout")
async def save_layout(session_id: str, body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Persist per-range node positions (RangeView drag; §4.4)."""
    rng = await app.state.db.get_range(session_id)
    if rng is None:
        raise HTTPException(status_code=404, detail="range not found")
    rng["layout"] = body.get("layout", {})
    await app.state.db.upsert_range(session_id, rng)
    return {"ok": True}


# --- multiplexed chat WebSocket (§7) ---------------------------------------


@app.websocket("/ws/chat")
async def ws_chat(websocket: WebSocket) -> None:
    """One socket for all tabs; each message carries its ``session_id``."""
    await websocket.accept()
    try:
        while True:
            raw = await websocket.receive_text()
            try:
                msg = json.loads(raw)
            except json.JSONDecodeError:
                continue
            session_id = msg.get("session_id")
            if not session_id:
                continue
            if msg.get("type") == "resume":
                await chat.replay(app, websocket, session_id)
                continue
            if msg.get("type") == "cancel":
                chat.cancel_session(session_id)
                continue
            content = (msg.get("content") or "").strip()
            if content:
                await chat.handle_message(app, websocket, session_id, content)
    except WebSocketDisconnect:
        return


# --- static frontend -------------------------------------------------------


def mount_frontend(frontend_dist: str) -> None:
    if os.path.isdir(frontend_dist):
        app.mount("/", StaticFiles(directory=frontend_dist, html=True), name="frontend")


_frontend_dist = os.environ.get("DREADGOAD_WEBAPP_FRONTEND_DIST")
if _frontend_dist:
    mount_frontend(_frontend_dist)
