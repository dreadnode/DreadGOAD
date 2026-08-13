"""Health, configuration, settings, and command-catalog HTTP routes."""

from __future__ import annotations

import os
import re
import typing as t

import yaml
from fastapi import APIRouter, HTTPException, Request

from . import __version__ as VERSION
from . import commands, configstore, labconfig, labs, paths

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
        "default_config_path": configstore.default_config_path(),
        "api_key_set": bool(os.environ.get("OPENROUTER_API_KEY")),
        # The create UI offers these and no others; sending the list keeps the
        # frontend from carrying its own copy that can drift from the backend's.
        "providers": list(configstore.PROVIDERS),
    }


@router.get("/api/configs")
async def get_configs(request: Request) -> dict[str, t.Any]:
    """List every config the console can attach to, for the new-session picker.

    Session anchors are read straight from the sessions table rather than kept
    in a registry of their own: the sessions *are* the record of which configs
    are in use, and a second list would be one more thing to keep in step with
    deletions.
    """
    sessions = await request.app.state.sessions.list_sessions()
    anchors = [(s.get("anchor") or {}).get("config_path") for s in sessions]
    configs = configstore.known_configs(p for p in anchors if p)
    return {
        "configs": configs,
        "configs_root": str(paths.configs_root()),
        "providers": list(configstore.PROVIDERS),
        "credential_hints": {
            provider: configstore.credential_hint(provider)
            for provider in configstore.PROVIDERS
        },
    }


@router.get("/api/labs")
async def get_labs(config_path: str | None = None) -> dict[str, t.Any]:
    """List labs available as a variant source, for the new-environment form.

    ``config_path`` is optional: the create-a-config flow needs this list before
    any config exists.
    """
    return {"labs": await labs.discover_labs(config_path)}


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


def _config_path_problem(config_path: str) -> str | None:
    """Explain why ``config_path`` can't be read, or None if it looks fine.

    Checked before parsing so the operator gets the sentence that matches what
    they did — a typo'd path, a directory, a stray quote — instead of the
    interpreter's phrasing of it. ``str(FileNotFoundError)`` renders as
    "[Errno 2] No such file or directory: '/path'", which leads with an errno
    and buries the path in quotes inside quotes.
    """
    path = config_path.strip()
    if not path:
        return "Config path is required."

    # A path pasted from a shell or a YAML file often keeps its quotes, and the
    # resulting "file not found" names a path that looks correct on screen.
    if len(path) >= 2 and path[0] in "\"'" and path[-1] == path[0]:
        return (
            f"Config path is wrapped in {path[0]} quotes — remove them and use "
            f"the bare path: {path[1:-1]}"
        )

    # Deliberately NOT expanded. This value is stored as the session's anchor
    # and handed to open() and to the Go CLI's --config, none of which expand
    # ~. Accepting it here would list the environments happily and then fail on
    # CREATE with the raw FileNotFoundError — the same error one step later,
    # which is worse than refusing it now. The expansion is offered as text so
    # it can be pasted straight back into the field.
    if path.startswith("~"):
        return (
            f"Config path must be a full path — ~ is not expanded here. "
            f"Use: {os.path.expanduser(path)}"
        )

    expanded = path
    if not os.path.isabs(expanded):
        return f"Config path must be absolute; got {path!r}."
    if os.path.isdir(expanded):
        return (
            f"{expanded} is a directory, not a config file. "
            "Point at the dreadgoad.yaml inside it."
        )
    if not os.path.exists(expanded):
        # Name the nearest existing ancestor: it separates "one typo in the
        # filename" from "this whole directory is wrong", which are different
        # things to go and check.
        parent = os.path.dirname(expanded) or "/"
        if os.path.isdir(parent):
            return (
                f"No such file: {expanded}. The directory {parent} exists — "
                "check the filename."
            )
        return f"No such file: {expanded}. The directory {parent} does not exist."
    if not os.access(expanded, os.R_OK):
        return f"{expanded} exists but is not readable (check permissions)."
    return None


@router.get("/api/environments")
async def get_environments(config_path: str) -> dict[str, t.Any]:
    """Return environment names defined in a configuration file."""
    problem = _config_path_problem(config_path)
    if problem is not None:
        raise HTTPException(status_code=400, detail=problem)
    try:
        # .strip() only: the path must stay byte-identical to what session
        # creation will store and open, or this endpoint validates something
        # other than what gets used.
        return labconfig.list_environments(config_path.strip())
    except yaml.YAMLError as exc:
        # A parse error's own message carries the line/column, which is the
        # useful part; label it so it is clear the file was found and read.
        raise HTTPException(
            status_code=400, detail=f"{config_path} is not valid YAML: {exc}"
        ) from exc
    except (FileNotFoundError, ValueError, OSError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
