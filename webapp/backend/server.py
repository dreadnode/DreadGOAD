"""FastAPI backend entry point.

Phase 0: minimal shell — health + config endpoints and static frontend mount.
Later phases add: SQLite persistence (§6), session lifecycle REST (§7),
multiplexed chat + range-state WebSockets, the agent, and the ingestion hook.
"""

from __future__ import annotations

import os
import typing as t

from fastapi import FastAPI
from fastapi.staticfiles import StaticFiles

from . import __version__ as VERSION

# Default model + provider (design decision: Sonnet 5 via OpenRouter).
_DEFAULT_MODEL = os.environ.get("DREADGOAD_WEBAPP_MODEL", "openrouter/anthropic/claude-sonnet-5")

app = FastAPI(title="DreadGOAD Web App")


@app.get("/api/health")
async def health() -> dict[str, t.Any]:
    """Liveness probe used by the launcher and Phase-0 manual test."""
    return {"status": "ok", "version": VERSION}


@app.get("/api/config")
async def get_config() -> dict[str, t.Any]:
    """Static app config for the frontend shell."""
    return {"version": VERSION, "default_model": _DEFAULT_MODEL}


def mount_frontend(frontend_dist: str) -> None:
    """Mount the built Vite frontend at ``/`` if the dist dir exists."""
    if os.path.isdir(frontend_dist):
        app.mount("/", StaticFiles(directory=frontend_dist, html=True), name="frontend")


# The launcher sets this to the built dist dir when serving in production mode.
_frontend_dist = os.environ.get("DREADGOAD_WEBAPP_FRONTEND_DIST")
if _frontend_dist:
    mount_frontend(_frontend_dist)
