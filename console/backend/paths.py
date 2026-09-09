"""Filesystem locations for the console (see design §10.2).

State lives under the gitignored ``.dreadgoad/console/`` runtime root:
  - ``state.db``               the SQLite DB
  - ``sessions/<slug>-<id>/``  per-session working dir (agent fs sandbox)
"""

from __future__ import annotations

import os
import stat
from pathlib import Path

_ENV_PREFIX = "DREADGOAD_CONSOLE_"
_LEGACY_ENV_PREFIX = "DREADGOAD_WEBAPP_"

# The model a session runs on when nothing else names one.
#
# Defined here, in the leaf module every consumer already imports, so the
# literal exists exactly once. It previously appeared in three places -- the
# API, the chat runtime, and the launcher's banner -- which is one value with
# three owners: change any one and the launcher prints a default the agent
# is not using.
FALLBACK_MODEL = "openrouter/anthropic/claude-sonnet-5"
PRIVATE_DIR_MODE = 0o700


def ensure_private_dir(path: str | Path) -> Path:
    """Create ``path`` and restrict it to the current OS user.

    ``mode=`` protects a newly created directory even under a permissive umask;
    ``chmod`` repairs directories created by older console versions.
    Permission failures propagate so the console never silently stores state in
    a location it failed to make private.
    """
    directory = Path(path)
    directory.mkdir(parents=True, exist_ok=True, mode=PRIVATE_DIR_MODE)
    if not stat.S_ISDIR(directory.lstat().st_mode):
        raise OSError(f"refusing non-directory console state path: {directory}")
    directory.chmod(PRIVATE_DIR_MODE)
    return directory


def setting(name: str, default: str | None = None) -> str | None:
    """Read a console setting from the environment, e.g. ``setting("PORT")``.

    Accepts the pre-rename ``DREADGOAD_WEBAPP_*`` spelling as a fallback, so a
    shell that already exports the old names keeps working. Empty values count
    as unset, which is what an exported-but-blank var means in practice.
    """
    return (
        os.environ.get(_ENV_PREFIX + name)
        or os.environ.get(_LEGACY_ENV_PREFIX + name)
        or default
    )


def default_model() -> str:
    """The model to use when a caller names none.

    ``DREADGOAD_CONSOLE_MODEL`` (or the legacy ``DREADGOAD_WEBAPP_MODEL``),
    else :data:`FALLBACK_MODEL`. Read on each call rather than captured at
    import, so a value exported after startup is still honoured.
    """
    return setting("MODEL") or FALLBACK_MODEL


def repo_root() -> Path:
    """Locate the DreadGOAD repo root (contains ``dreadgoad.yaml`` / ``ad/``).

    Walks up from this file; falls back to the cwd. The CLI is invoked with
    ``cwd = repo_root`` so it can read ``ad/``, ``infra/``, ``dreadgoad.yaml``.
    """
    here = Path(__file__).resolve()
    # The repo root is the ancestor dir containing the lab definitions (``ad/``).
    for parent in here.parents:
        if (parent / "ad").is_dir():
            return parent
    return Path.cwd()


def state_root() -> Path:
    """``.dreadgoad/console/`` under the repo root, created if missing.

    Overridable via ``DREADGOAD_CONSOLE_STATE_ROOT`` (used by tests to isolate
    the DB and session dirs from the repo).

    Migrates the pre-rename ``.dreadgoad/webapp/`` root on first use so existing
    sessions, chat history and range state survive. Only when the new root does
    not exist, so newer data can never be clobbered; a failed move is not fatal
    (we just start fresh rather than refusing to boot).
    """
    override = setting("STATE_ROOT")
    if override:
        root = Path(override)
    else:
        root = repo_root() / ".dreadgoad" / "console"
        legacy = repo_root() / ".dreadgoad" / "webapp"
        if legacy.is_dir() and not root.exists():
            root.parent.mkdir(parents=True, exist_ok=True)
            try:
                legacy.rename(root)
            except OSError:
                pass
    return ensure_private_dir(root)


def db_path() -> Path:
    """Absolute path to the SQLite state DB."""
    return state_root() / "state.db"


def sessions_root() -> Path:
    """``.dreadgoad/console/sessions/`` — per-session working dirs live here."""
    return ensure_private_dir(state_root() / "sessions")


def configs_root() -> Path:
    """``.dreadgoad/console/configs/`` — configs the console itself created.

    Under the state root rather than beside it so the existing
    ``DREADGOAD_CONSOLE_STATE_ROOT`` override isolates tests from the real repo,
    and so everything the console writes lives in one place.

    Two properties matter for what lands here. It is gitignored (``.gitignore``
    line 17), so a ludus ``api_key`` or proxmox ``password`` written into one of
    these cannot be committed by accident — unlike the repo-root
    ``dreadgoad.yaml``, which is tracked. And it sits under the repo, so the
    CLI's project-root walk finds ``ansible/`` above it and resolves inventory
    and lab data in the right tree (see projectroot.py). Overriding the state
    root to somewhere outside the repo breaks that second property;
    ``projectroot.preflight`` detects it and warns rather than failing silently.
    """
    return ensure_private_dir(state_root() / "configs")


def session_dir(dirname: str) -> Path:
    """Working dir for a session (``<slug>-<shortid>``), created if missing."""
    return ensure_private_dir(sessions_root() / dirname)


# Allow overriding the DB path in tests via env var.
def resolve_db_path() -> str:
    """The DB path, with ``DREADGOAD_CONSOLE_DB`` overriding the default."""
    return setting("DB") or str(db_path())
