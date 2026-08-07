"""Health, configuration, settings, and command-catalog HTTP routes."""

from __future__ import annotations

import os
import re
import typing as t

import yaml
from fastapi import APIRouter, HTTPException

from . import __version__ as VERSION
from . import commands, labconfig, paths

router = APIRouter()

DEFAULT_MODEL = paths.setting("MODEL") or "openrouter/anthropic/claude-sonnet-5"

# Settings may only write credential-shaped variables. Allowing arbitrary names
# could alter PATH/LD_PRELOAD and hijack subprocesses launched by the console.
_API_KEY_ENV_RE = re.compile(r"^[A-Z][A-Z0-9_]*_(?:API_KEY|KEY|TOKEN)$")


@router.get("/api/health")
async def health() -> dict[str, t.Any]:
    """Return liveness information for the console itself."""
    return {"status": "ok", "version": VERSION}


@router.get("/api/config")
async def get_config() -> dict[str, t.Any]:
    """Return bootstrap values needed before a session exists."""
    return {
        "version": VERSION,
        "default_model": DEFAULT_MODEL,
        "default_config_path": str(paths.repo_root() / "dreadgoad.yaml"),
        "api_key_set": bool(os.environ.get("OPENROUTER_API_KEY")),
    }


@router.post("/api/settings")
async def update_settings(body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Set an LLM API key in memory without returning or persisting it."""
    api_key = (body.get("api_key") or "").strip()
    api_key_env = (body.get("api_key_env") or "OPENROUTER_API_KEY").strip()
    if not api_key_env:
        raise HTTPException(status_code=400, detail="api_key_env is required")
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


@router.get("/api/commands")
async def get_commands() -> dict[str, t.Any]:
    """Return the slash-command registry for frontend autocomplete."""
    return {"commands": commands.command_catalog()}


@router.get("/api/environments")
async def get_environments(config_path: str) -> dict[str, t.Any]:
    """Return environment names defined in a configuration file."""
    try:
        return labconfig.list_environments(config_path)
    except (FileNotFoundError, ValueError, OSError, yaml.YAMLError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
