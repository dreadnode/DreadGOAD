"""FastAPI backend entry point.

Phase 0: health/config + static mount.
Phase 2: SQLite-backed session lifecycle REST + RangeView reads.
Later: multiplexed chat + range-state WebSockets, the agent, ingestion hook.
"""

from __future__ import annotations

import json
import os
import re
import typing as t
from contextlib import asynccontextmanager

import yaml
from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.staticfiles import StaticFiles

from . import __version__ as VERSION
from . import chat, commands, labconfig, paths
from .db import Database
from .sessions import SessionService

# Default model + provider (design decision: Sonnet 5 via OpenRouter).
_DEFAULT_MODEL = paths.setting("MODEL") or "openrouter/anthropic/claude-sonnet-5"

# Env vars the settings endpoint may write — key/token names only, so it can't
# clobber PATH/LD_PRELOAD/etc. and hijack the CLI's subprocesses.
_API_KEY_ENV_RE = re.compile(r"^[A-Z][A-Z0-9_]*_(?:API_KEY|KEY|TOKEN)$")

# Browsers do NOT apply the same-origin policy to WebSocket handshakes, so
# without this check any page an operator happens to visit could open a socket
# to the console on loopback and drive it — including /destroy. The port is
# deliberately not pinned: in --dev mode the browser's origin is vite's port
# and its proxy forwards that header through unchanged.
_WS_ORIGIN_RE = re.compile(
    r"^(?:https?|wss?)://(?:localhost|127\.0\.0\.1|\[::1\])(?::\d+)?$"
)


def ws_origin_allowed(origin: str | None) -> bool:
    """Whether a WebSocket handshake's ``Origin`` may open a console socket.

    ``None`` means the client isn't a browser (curl, a test client, a script) —
    it never had cross-origin authority to abuse, so it is allowed. Anything
    else must be a loopback origin, on any port.
    """
    if origin is None:
        return True
    return bool(_WS_ORIGIN_RE.match(origin.strip().lower()))


async def reconcile_interrupted(db: Database) -> int:
    """Flip sessions left mid-deploy by a crash/restart to ``interrupted`` (§5.4).

    Returns the number of sessions reconciled.
    """
    n = 0
    for s in await db.list_sessions():
        if s.get("status") == "provisioning":
            s["status"] = "interrupted"
            await db.upsert_session(s)
            n += 1
    return n


@asynccontextmanager
async def _lifespan(app: FastAPI) -> t.AsyncIterator[None]:
    """Open the DB and build the session service on startup."""
    db = await Database(paths.resolve_db_path()).connect()
    await db.set_meta("schema_version", 1)
    app.state.db = db
    app.state.sessions = SessionService(
        db, repo_root=str(paths.repo_root()), sessions_root=paths.sessions_root()
    )
    # Survival: reconcile sessions left mid-deploy by a crash/restart (§5.4).
    await reconcile_interrupted(db)
    try:
        yield
    finally:
        await db.close()


app = FastAPI(title="DreadGOAD Console", lifespan=_lifespan)


# --- health / config -------------------------------------------------------


@app.get("/api/health")
async def health() -> dict[str, t.Any]:
    """Liveness probe for the console itself (not the range)."""
    return {"status": "ok", "version": VERSION}


@app.get("/api/config")
async def get_config() -> dict[str, t.Any]:
    """Bootstrap values the SPA needs before any session exists."""
    return {
        "version": VERSION,
        "default_model": _DEFAULT_MODEL,
        "default_config_path": str(paths.repo_root() / "dreadgoad.yaml"),
        # Whether the default provider's key is present, so the UI can prompt.
        "api_key_set": bool(os.environ.get("OPENROUTER_API_KEY")),
    }


@app.post("/api/settings")
async def update_settings(body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Set the LLM API key at runtime (in-memory env only — never persisted).

    ``api_key`` is stored into the env var named by ``api_key_env`` (default
    ``OPENROUTER_API_KEY``). With no ``api_key``, the named var must already be
    set. The key value is never returned.
    """
    api_key = (body.get("api_key") or "").strip()
    api_key_env = (body.get("api_key_env") or "OPENROUTER_API_KEY").strip()
    if not api_key_env:
        raise HTTPException(status_code=400, detail="api_key_env is required")
    # Restrict to key/token-shaped var names so this can't overwrite PATH,
    # LD_PRELOAD, etc. (the CLI shells out to terraform/aws/az via PATH).
    if not _API_KEY_ENV_RE.match(api_key_env):
        raise HTTPException(
            status_code=400,
            detail=(
                f"api_key_env must be an API-key/token variable "
                f"(e.g. *_API_KEY, *_KEY, *_TOKEN); got {api_key_env!r}"
            ),
        )
    if api_key:
        os.environ[api_key_env] = api_key
    elif not os.environ.get(api_key_env):
        raise HTTPException(
            status_code=400, detail=f"{api_key_env} is not set; provide an api_key"
        )
    return {"ok": True, "api_key_env": api_key_env}


@app.get("/api/commands")
async def get_commands() -> dict[str, t.Any]:
    """The slash-command registry, for the frontend autocomplete menu."""
    return {"commands": commands.command_catalog()}


@app.get("/api/environments")
async def get_environments(config_path: str) -> dict[str, t.Any]:
    """Environment names defined in a config file — drives the new-session dropdown."""
    try:
        return labconfig.list_environments(config_path)
    except (FileNotFoundError, ValueError, OSError, yaml.YAMLError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


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

    try:
        if mode == "new":
            env_fields = body.get("env_fields") or {}
            top_level = body.get("top_level")
            return await svc.create_new_env_session(
                config_path,
                env,
                env_fields,
                top_level=top_level,
                model=model,
                label=label,
            )
        return await svc.create_session(config_path, env, model=model, label=label)
    except (FileNotFoundError, ValueError, yaml.YAMLError) as exc:
        # Invalid config path / malformed yaml / unknown env → client error.
        raise HTTPException(status_code=400, detail=str(exc)) from exc


@app.get("/api/sessions")
async def list_sessions() -> dict[str, t.Any]:
    """List every session, for the tab bar."""
    return {"sessions": await _svc(app).list_sessions()}


@app.get("/api/sessions/{session_id}")
async def get_session(session_id: str) -> dict[str, t.Any]:
    """Fetch one session. 404 if unknown."""
    s = await _svc(app).get_session(session_id)
    if s is None:
        raise HTTPException(status_code=404, detail="session not found")
    return s


@app.put("/api/sessions/{session_id}/model")
async def set_model(session_id: str, body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Switch a session's agent model in place (conversation continues)."""
    model = (body.get("model") or "").strip()
    if not model:
        raise HTTPException(status_code=400, detail="model is required")
    session = await chat.swap_model(app, session_id, model)
    if session is None:
        raise HTTPException(status_code=404, detail="session not found")
    return {"ok": True, "model": model}


@app.delete("/api/sessions/{session_id}")
async def delete_session(session_id: str) -> dict[str, t.Any]:
    """Delete a session, its working dir, and its in-memory runtime state."""
    ok = await _svc(app).delete_session(session_id)
    if not ok:
        raise HTTPException(status_code=404, detail="session not found")
    chat.cleanup_session(session_id)  # evict agent/lock, cancel in-flight (F4)
    return {"deleted": session_id}


# --- RangeView reads (§7) --------------------------------------------------


@app.get("/api/ranges/{session_id}")
async def get_range(session_id: str) -> dict[str, t.Any]:
    """The range topology RangeView renders; re-fetched after each check_run."""
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
    if not ws_origin_allowed(websocket.headers.get("origin")):
        # 1008 = policy violation. Refuse before accept() so the handshake
        # itself fails and the page never gets a usable socket.
        await websocket.close(code=1008)
        return
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
            # This socket is now the session's live target (re-attach on reconnect).
            chat.register_conn(session_id, websocket)
            if msg.get("type") == "resume":
                await chat.replay(app, session_id)
                continue
            if msg.get("type") == "cancel":
                chat.cancel_session(session_id)
                continue
            content = (msg.get("content") or "").strip()
            if content:
                # Non-blocking: run the turn in a per-session task so the recv
                # loop stays free for cancel/other sessions (§5.4, §7).
                chat.dispatch(app, session_id, content)
    except WebSocketDisconnect:
        # In-flight turns keep running server-side (tracked in chat._tasks) and
        # emit to the session's current socket; a reconnect re-attaches.
        chat.unregister_conn(websocket)
        return


# --- static frontend -------------------------------------------------------


def mount_frontend(frontend_dist: str) -> None:
    """Serve the built SPA at ``/``. No-op if the dist dir doesn't exist."""
    if os.path.isdir(frontend_dist):
        app.mount("/", StaticFiles(directory=frontend_dist, html=True), name="frontend")


_frontend_dist = paths.setting("FRONTEND_DIST")
if _frontend_dist:
    mount_frontend(_frontend_dist)
