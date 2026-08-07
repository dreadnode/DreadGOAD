"""FastAPI application assembly and lifecycle for the DreadGOAD console."""

from __future__ import annotations

import os
import typing as t
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles

from . import chat, paths
from .chat_socket import (  # noqa: F401 -- compatibility re-exports
    WS_MAX_CONTENT_CHARS,
    WS_MAX_MESSAGE_CHARS,
    WS_MAX_SESSION_ID_CHARS,
    parse_ws_message,
    router as chat_router,
    ws_chat,
    ws_origin_allowed,
)
from .config_routes import (  # noqa: F401 -- compatibility re-exports
    DEFAULT_MODEL as _DEFAULT_MODEL,
    get_commands,
    get_config,
    get_environments,
    health,
    router as config_router,
    update_settings,
)
from .db import Database
from .range_routes import (  # noqa: F401 -- compatibility re-exports
    LAYOUT_MAX_ABS_COORDINATE,
    LAYOUT_MAX_NODES,
    get_range,
    router as range_router,
    save_layout,
)
from .session_routes import (  # noqa: F401 -- compatibility re-exports
    create_session,
    delete_session,
    get_session,
    list_sessions,
    router as session_router,
    set_model,
)
from .sessions import SessionService


async def reconcile_interrupted(db: Database) -> int:
    """Mark sessions left provisioning by a prior process as interrupted."""
    reconciled = 0
    for session in await db.list_sessions():
        if session.get("status") == "provisioning":
            session["status"] = "interrupted"
            await db.upsert_session(session)
            reconciled += 1
    return reconciled


@asynccontextmanager
async def _lifespan(app: FastAPI) -> t.AsyncIterator[None]:
    """Open persistence before serving and stop runtime work before closing it."""
    db = await Database(paths.resolve_db_path()).connect()
    await db.set_meta("schema_version", 1)
    app.state.db = db
    app.state.sessions = SessionService(
        db, repo_root=str(paths.repo_root()), sessions_root=paths.sessions_root()
    )
    await reconcile_interrupted(db)
    try:
        yield
    finally:
        try:
            await chat.cleanup_all()
        finally:
            await db.close()


app = FastAPI(title="DreadGOAD Console", lifespan=_lifespan)
app.include_router(config_router)
app.include_router(session_router)
app.include_router(range_router)
app.include_router(chat_router)


def mount_frontend(frontend_dist: str) -> None:
    """Serve the built SPA at ``/`` when its distribution directory exists."""
    if os.path.isdir(frontend_dist):
        app.mount("/", StaticFiles(directory=frontend_dist, html=True), name="frontend")


_frontend_dist = paths.setting("FRONTEND_DIST")
if _frontend_dist:
    mount_frontend(_frontend_dist)
