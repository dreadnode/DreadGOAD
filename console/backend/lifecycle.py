"""Generic range-owned initialization for newly attached console sessions."""

from __future__ import annotations

import json
import logging
import typing as t
from pathlib import Path

from . import commands, projectroot
from .cli import Capture, capture
from .schemas import SessionDocument

log = logging.getLogger(__name__)


class InitResult(t.TypedDict):
    """One allowlisted initialization action reported by the CLI."""

    action: str
    status: str
    artifacts: t.NotRequired[list[str]]
    message: t.NotRequired[str]


def artifacts_dir(session: SessionDocument) -> Path:
    """Return the private, session-local destination for generated artifacts."""
    return Path(session["session_dir"]) / "artifacts"


def answer_key_path(session: t.Mapping[str, t.Any]) -> Path:
    """Return the deterministic answer-key artifact path for a session."""
    return Path(str(session["session_dir"])) / "artifacts" / "answer_key.json"


async def initialize_session(
    session: SessionDocument,
    fallback_root: str,
    capture_command: Capture | None = None,
) -> list[InitResult]:
    """Ask the selected range to run its declared session initialization.

    Initialization is deliberately non-fatal to session creation. Failures are
    returned as structured results so the session service can retain and show
    them; a broken optional initializer must not make a usable range disappear.
    """
    anchor = session.get("anchor") or {}
    config_path = anchor.get("config_path")
    env = anchor.get("env")
    if not config_path or not env:
        return [
            {
                "action": "range_init",
                "status": "failed",
                "message": "session has no config/environment anchor",
            }
        ]

    root, _ = projectroot.resolve_root(config_path)
    argv = [
        # Use the console checkout's binary, which implements this protocol,
        # while running it in the selected config's tree so the CLI resolves
        # that range's manifest and data.
        commands.resolve_bin(fallback_root),
        "--config",
        str(config_path),
        "--env",
        str(env),
        "range",
        "init-session",
        "--output-dir",
        str(artifacts_dir(session)),
        "--json",
    ]
    runner = capture_command or capture
    try:
        rc, stdout, stderr = await runner(argv, str(root))
    except (OSError, ValueError) as exc:
        log.warning("range session initialization failed to start: %s", exc)
        return [{"action": "range_init", "status": "failed", "message": str(exc)}]

    results = _parse_results(stdout)
    if results is None or (rc != 0 and not results):
        message = (stderr or stdout or f"initializer exited {rc}").strip()
        log.warning("invalid range session initialization response: %s", message)
        return [
            {
                "action": "range_init",
                "status": "failed",
                "message": message,
            }
        ]
    return results


def _parse_results(output: str) -> list[InitResult] | None:
    """Validate the small JSON contract emitted by ``range init-session``."""
    try:
        payload = json.loads(output)
    except (TypeError, ValueError):
        return None
    actions = payload.get("actions") if isinstance(payload, dict) else None
    if not isinstance(actions, list):
        return None

    results: list[InitResult] = []
    for raw in actions:
        if not isinstance(raw, dict):
            return None
        action, status = raw.get("action"), raw.get("status")
        if not isinstance(action, str) or status not in {
            "completed",
            "pending",
            "skipped",
            "failed",
        }:
            return None
        result: InitResult = {"action": action, "status": status}
        message = raw.get("message")
        if isinstance(message, str) and message:
            result["message"] = message
        artifacts = raw.get("artifacts")
        if isinstance(artifacts, list) and all(
            isinstance(path, str) for path in artifacts
        ):
            result["artifacts"] = artifacts
        elif artifacts is not None:
            return None
        results.append(result)
    return results
