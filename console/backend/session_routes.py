"""Session lifecycle HTTP routes."""

from __future__ import annotations

import typing as t

import yaml
from fastapi import APIRouter, HTTPException, Request
from pydantic import BaseModel, Field

from . import chat, configstore, paths
from .schemas import SessionDocument
from .sessions import SessionService

router = APIRouter()


class SessionRequestBase(BaseModel):
    """Fields shared by every session-creation request."""

    env: str = ""
    model: str | None = None
    label: str | None = None


class AttachSessionRequest(SessionRequestBase):
    """Attach a console session to an environment in an existing config."""

    mode: t.Literal["attach"] = "attach"
    config_path: str | None = None


class NewEnvironmentSessionRequest(SessionRequestBase):
    """Create an environment in an existing config and attach to it."""

    mode: t.Literal["new"]
    config_path: str | None = None
    env_fields: dict[str, t.Any] = Field(default_factory=dict)
    top_level: dict[str, t.Any] | None = None


class NewConfigSessionRequest(SessionRequestBase):
    """Create a managed config with its first environment and session."""

    mode: t.Literal["new_config"]
    config_name: str | None = None
    provider: str
    region: str | None = None
    env_fields: dict[str, t.Any] = Field(default_factory=dict)


class CreateRangeSessionRequest(SessionRequestBase):
    """Create a range-first managed environment and session."""

    mode: t.Literal["create_range"]
    range: str
    provider: str
    region: str | None = None
    customization: t.Literal["standard", "randomized"] = "standard"
    vpc_cidr: str | None = None


SessionCreateRequest = (
    AttachSessionRequest
    | NewEnvironmentSessionRequest
    | NewConfigSessionRequest
    | CreateRangeSessionRequest
)


def _service(request: Request) -> SessionService:
    return request.app.state.sessions


def _trim_optional(value: str | None) -> str | None:
    """Trim an optional request string and normalize blank values to ``None``."""
    if value is None:
        return None
    return value.strip() or None


@router.post("/api/sessions")
async def create_session(
    request: Request, body: SessionCreateRequest
) -> SessionDocument:
    """Create a session by attaching to, or creating, an environment."""
    service = _service(request)
    env = body.env.strip()
    if not env:
        raise HTTPException(status_code=400, detail="env is required")
    model = body.model or paths.default_model()
    label = body.label

    try:
        if isinstance(body, CreateRangeSessionRequest):
            return await service.create_range_session(
                body.range,
                body.provider,
                env,
                region=_trim_optional(body.region),
                customization=body.customization,
                vpc_cidr=_trim_optional(body.vpc_cidr),
                model=model,
                label=label,
            )
        if isinstance(body, NewConfigSessionRequest):
            provider = body.provider.strip()
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
                body.config_name or env,
                provider,
                env,
                body.env_fields,
                region=_trim_optional(body.region),
                model=model,
                label=label,
            )
        if isinstance(body, NewEnvironmentSessionRequest):
            config_path = body.config_path or str(paths.repo_root() / "dreadgoad.yaml")
            return await service.create_new_env_session(
                config_path,
                env,
                body.env_fields,
                top_level=body.top_level,
                model=model,
                label=label,
            )
        config_path = body.config_path or str(paths.repo_root() / "dreadgoad.yaml")
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
async def get_session(request: Request, session_id: str) -> SessionDocument:
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
        chat.release_cleanup(session_id)
    except Exception:
        chat.release_cleanup(session_id)
        raise
    return {"deleted": session_id}
