"""Filesystem locations for the web app (see design §10.2).

State lives under the gitignored ``.dreadgoad/webapp/`` runtime root:
  - ``state.db``               the SQLite DB
  - ``sessions/<slug>-<id>/``  per-session working dir (agent fs sandbox)
"""

from __future__ import annotations

import os
from pathlib import Path


def repo_root() -> Path:
    """Locate the DreadGOAD repo root (contains ``dreadgoad.yaml`` / ``ad/``).

    Walks up from this file; falls back to the cwd. The CLI is invoked with
    ``cwd = repo_root`` so it can read ``ad/``, ``infra/``, ``dreadgoad.yaml``.
    """
    here = Path(__file__).resolve()
    for parent in [here, *here.parents]:
        if (parent / "dreadgoad.yaml").is_file() or (parent / "ad").is_dir():
            if (parent / "ad").is_dir():
                return parent
    return Path.cwd()


def state_root() -> Path:
    """``.dreadgoad/webapp/`` under the repo root, created if missing.

    Overridable via ``DREADGOAD_WEBAPP_STATE_ROOT`` (used by tests to isolate
    the DB and session dirs from the repo).
    """
    override = os.environ.get("DREADGOAD_WEBAPP_STATE_ROOT")
    root = Path(override) if override else repo_root() / ".dreadgoad" / "webapp"
    root.mkdir(parents=True, exist_ok=True)
    return root


def db_path() -> Path:
    """Absolute path to the SQLite state DB."""
    return state_root() / "state.db"


def sessions_root() -> Path:
    """``.dreadgoad/webapp/sessions/`` — per-session working dirs live here."""
    root = state_root() / "sessions"
    root.mkdir(parents=True, exist_ok=True)
    return root


def session_dir(dirname: str) -> Path:
    """Working dir for a session (``<slug>-<shortid>``), created if missing."""
    d = sessions_root() / dirname
    d.mkdir(parents=True, exist_ok=True)
    return d


# Allow overriding the DB path in tests via env var.
def resolve_db_path() -> str:
    return os.environ.get("DREADGOAD_WEBAPP_DB", str(db_path()))
