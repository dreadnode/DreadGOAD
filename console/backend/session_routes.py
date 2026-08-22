"""Session lifecycle HTTP routes."""

from __future__ import annotations

import typing as t

import yaml
from fastapi import APIRouter, HTTPException, Request

from . import chat, configstore, paths
from .sessions import SessionService

router = APIRouter()


def _service(request: Request) -> SessionService:
    return request.app.state.sessions


@router.post("/api/sessions")
async def create_session(request: Request, body: dict[str, t.Any]) -> dict[str, t.Any]:
    """Create a session by attaching to, or creating, an environment."""
    service = _service(request)
    mode = body.get("mode", "attach")
    config_path = body.get("config_path") or str(paths.repo_root() / "dreadgoad.yaml")
    env = (body.get("env") or "").strip()
    if not env:
        raise HTTPException(status_code=400, detail="env is required")
    model = body.get("model") or paths.default_model()
    label = body.get("label")

    try:
        if mode == "new_config":
            provider = (body.get("provider") or "").strip()
            if provider not in configstore.PROVIDERS:
                raise HTTPException(
                    status_code=400,
                    detail=(
                        f"provider must be one of "
                        f"{', '.join(configstore.PROVIDERS)}; got {provider!r}. "
                        f"proxmox and ludus are supported by the CLI but not yet "
                        f"by the console."
                    ),
                )
            return await service.create_config_session(
                body.get("config_name") or env,
                provider,
                env,
                body.get("env_fields") or {},
                region=(body.get("region") or "").strip() or None,
                model=model,
                label=label,
            )
        if mode == "new":
            return await service.create_new_env_session(
                config_path,
                env,
                body.get("env_fields") or {},
                top_level=body.get("top_level"),
                model=model,
                label=label,
            )
        return await service.create_session(config_path, env, model=model, label=label)
    except FileExistsError as exc:
        # 409, not 400: the request was well-formed and the name is simply
        # taken, which is the one failure here the operator fixes by renaming
        # rather than by correcting what they typed.
        raise HTTPException(status_code=409, detail=str(exc)) from exc
    except (FileNotFoundError, ValueError, yaml.YAMLError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


@router.get("/api/sessions")
async def list_sessions(request: Request) -> dict[str, t.Any]:
    """List every session for the tab bar."""
    return {"sessions": await _service(request).list_sessions()}


@router.get("/api/sessions/{session_id}")
async def get_session(request: Request, session_id: str) -> dict[str, t.Any]:
    """Return one session or a 404 when it does not exist."""
    session = await _service(request).get_session(session_id)
    if session is None:
        raise HTTPException(status_code=404, detail="session not found")
    return session


@router.put("/api/sessions/{session_id}/model")
async def set_model(
    request: Request, session_id: str, body: dict[str, t.Any]
) -> dict[str, t.Any]:
    """Switch a session's model while preserving its conversation."""
    model = (body.get("model") or "").strip()
    if not model:
        raise HTTPException(status_code=400, detail="model is required")
    session = await chat.swap_model(request.app, session_id, model)
    if session is None:
        raise HTTPException(status_code=404, detail="session not found")
    return {"ok": True, "model": model}


@router.delete("/api/sessions/{session_id}")
async def delete_session(request: Request, session_id: str) -> dict[str, t.Any]:
    """Delete a session, its working directory, and its runtime state."""
    service = _service(request)
    if await service.get_session(session_id) is None:
        raise HTTPException(status_code=404, detail="session not found")

    # Reservation and the active-turn check are synchronous, so WebSocket
    # dispatch cannot slip between them on this event loop.
    if not chat.begin_cleanup(session_id):
        raise HTTPException(
            status_code=409,
            detail="session has an active turn; cancel it and wait before deleting",
        )
    try:
        await chat.cleanup_session(session_id)
        if not await service.delete_session(session_id):
            raise HTTPException(status_code=404, detail="session not found")
    except Exception:
        chat.release_cleanup(session_id)
        raise
    return {"deleted": session_id}
