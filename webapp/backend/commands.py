"""Slash-command registry + dreadgoad argv builder (design §5).

Each command maps to an exact CLI verb. Commands are provider-agnostic — the
session's ``(config_path, env)`` anchor is injected as global flags; provider
comes from the config file, so no ``--provider`` is needed. All CLI calls run
with ``cwd = repo root`` (see runner in cli.py).
"""

from __future__ import annotations

import shutil
import typing as t
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Command:
    name: str
    verb: tuple[str, ...]           # base CLI verb after `dreadgoad`
    dispatch: str = "direct"        # "direct" (deterministic) | "agent"
    long_running: bool = False      # streamed + guarded cancel (§5.4)
    takes_args: bool = False
    description: str = ""


# The 14 slash commands (§5.2). Dispatch defaults to "direct"; free-text
# prompts (not commands) are what route through the agent in v1.
REGISTRY: dict[str, Command] = {
    "/up":         Command("/up", ("up",), long_running=True, description="Full bring-up"),
    "/provision":  Command("/provision", ("provision",), long_running=True, description="Re-run config playbooks"),
    "/reset":      Command("/reset", ("lab", "reset"), long_running=True, description="Restore AD baseline"),
    "/start":      Command("/start", ("lab", "start"), description="Power on"),
    "/stop":       Command("/stop", ("lab", "stop"), description="Power off"),
    "/destroy":    Command("/destroy", ("infra", "destroy"), long_running=True, description="Tear down infra"),
    "/instances":  Command("/instances", ("lab", "status", "--json"), description="Cloud power state"),
    "/health":     Command("/health", ("health-check",), long_running=True, description="AD functional health"),
    "/validate":   Command("/validate", ("validate",), long_running=True, description="Vuln-config correctness"),
    "/diagnose":   Command("/diagnose", ("diagnose",), long_running=True, description="DC connectivity drill-down"),
    "/score":      Command("/score", ("score",), takes_args=True, description="Score an agent report"),
    "/scrub":      Command("/scrub", ("score", "reset"), description="Clean agent artifacts"),
    "/variant":    Command("/variant", ("variant", "generate"), takes_args=True, description="Generate a variant"),
    "/extensions": Command("/extensions", ("extension",), takes_args=True, description="List/provision extensions"),
}


def resolve_bin(repo_root: str | Path) -> str:
    """Locate the dreadgoad binary: PATH first, then ``cli/dreadgoad``."""
    found = shutil.which("dreadgoad")
    if found:
        return found
    return str(Path(repo_root) / "cli" / "dreadgoad")


def _verb_for(cmd: Command, extra: list[str]) -> tuple[list[str], list[str]]:
    """Resolve a command's concrete verb + trailing args from chat args.

    Handles the arg-shaped commands:
      - /extensions  → `extension list` (no arg) or `extension provision <name>`
      - /score       → `score --report <path>` (+ any flags like --live-verify)
      - /variant     → `variant generate <flags…>`
    """
    if cmd.name == "/extensions":
        if extra:
            return ["extension", "provision", extra[0]], extra[1:]
        return ["extension", "list"], []
    if cmd.name == "/score":
        if extra:
            return ["score", "--report", extra[0]], extra[1:]
        return ["score"], []
    return list(cmd.verb), extra


def build_argv(
    session: dict[str, t.Any],
    name: str,
    extra_args: list[str] | None = None,
    repo_root: str | Path = ".",
) -> list[str]:
    """Build the full dreadgoad argv for a command in a session's context.

    Shape: ``[bin, --config <abs>, --env <env>, <verb…>, <extra…>]``.
    """
    if name not in REGISTRY:
        raise KeyError(f"unknown command: {name}")
    cmd = REGISTRY[name]
    anchor = session["anchor"]
    verb, trailing = _verb_for(cmd, list(extra_args or []))
    return [
        resolve_bin(repo_root),
        "--config", str(anchor["config_path"]),
        "--env", str(anchor["env"]),
        *verb,
        *trailing,
    ]


def is_command(text: str) -> bool:
    return text.strip().split(" ", 1)[0] in REGISTRY


def parse_command(text: str) -> tuple[str, list[str]]:
    """Split ``/cmd arg1 arg2`` → ("/cmd", ["arg1", "arg2"])."""
    parts = text.strip().split()
    return parts[0], parts[1:]
