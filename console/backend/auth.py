"""Per-launch authentication for the loopback console control plane."""

from __future__ import annotations

import os
import secrets

from . import paths

AUTH_ENV = "DREADGOAD_CONSOLE_AUTH_TOKEN"
LEGACY_AUTH_ENV = "DREADGOAD_WEBAPP_AUTH_TOKEN"
MIN_TOKEN_CHARS = 32
WS_PROTOCOL = "dreadgoad.auth"
WS_CREDENTIAL_PROTOCOL_PREFIX = "dreadgoad.token."


def _load_token() -> str:
    """Capture the launch token, then remove it from child-process inheritance."""
    configured = paths.setting("AUTH_TOKEN")
    if configured is None:
        configured = secrets.token_urlsafe(32)
    if len(configured) < MIN_TOKEN_CHARS:
        raise RuntimeError(
            f"{AUTH_ENV} must contain at least {MIN_TOKEN_CHARS} characters"
        )
    # The server has captured the secret. CLI/cloud subprocesses launched later
    # do not need it and should not inherit it.
    os.environ.pop(AUTH_ENV, None)
    os.environ.pop(LEGACY_AUTH_ENV, None)
    return configured


_TOKEN = _load_token()


def authorization_value() -> str:
    """Return the bearer value expected on authenticated HTTP requests."""
    return f"Bearer {_TOKEN}"


def bearer_authorized(value: str | None) -> bool:
    """Validate one Authorization header without leaking comparison timing."""
    if value is None:
        return False
    scheme, separator, credential = value.partition(" ")
    if not separator or scheme.lower() != "bearer" or not credential:
        return False
    if credential != credential.strip() or " " in credential:
        return False
    return secrets.compare_digest(credential, _TOKEN)


def websocket_protocol_authorized(value: str | None) -> bool:
    """Validate the token carried in browser WebSocket subprotocol offers."""
    if value is None:
        return False
    protocols = [part.strip() for part in value.split(",")]
    if WS_PROTOCOL not in protocols:
        return False
    for protocol in protocols:
        if protocol.startswith(WS_CREDENTIAL_PROTOCOL_PREFIX):
            candidate = protocol.removeprefix(WS_CREDENTIAL_PROTOCOL_PREFIX)
            if candidate and secrets.compare_digest(candidate, _TOKEN):
                return True
    return False
