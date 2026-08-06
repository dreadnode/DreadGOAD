"""Slash-command registry + dreadgoad argv builder (design §5).

Each command maps to an exact CLI verb. Commands are provider-agnostic — the
session's ``(config_path, env)`` anchor is injected as global flags; provider
comes from the config file, so no ``--provider`` is needed. All CLI calls run
with ``cwd = repo root`` (see runner in cli.py).
"""

from __future__ import annotations

import os
import shlex
import shutil
import typing as t
from dataclasses import dataclass
from pathlib import Path

# Prompt content lives beside this module as editable markdown (design §5.1):
#   prompts/system.md         the agent's system prompt ($placeholder template)
#   prompts/<command>.md      optional per-command guidance for agent commands
_PROMPTS_DIR = Path(__file__).resolve().parent / "prompts"


def load_prompt(stem: str) -> str | None:
    """Read ``prompts/<stem>.md`` (stripped), or ``None`` if absent.

    ``stem`` is a command name without its slash (e.g. ``"variant"``) for
    per-command guidance, or ``"system"`` for the shared system prompt.
    """
    try:
        return (_PROMPTS_DIR / f"{stem}.md").read_text(encoding="utf-8").strip()
    except FileNotFoundError:
        return None


@dataclass(frozen=True)
class Command:
    """One slash command and how it maps onto the dreadgoad CLI."""

    name: str
    verb: tuple[str, ...]  # base CLI verb after `dreadgoad`
    dispatch: str = "direct"  # "direct" (deterministic) | "agent"
    long_running: bool = False  # streamed + guarded cancel (§5.4)
    takes_args: bool = False
    description: str = ""  # what it does, one line, in the autocomplete menu
    # The consequence an operator needs *before* pressing enter: what it costs,
    # what it destroys, or what it depends on. Wording is taken from the CLI's
    # own command help so the two can't drift into disagreeing.
    detail: str = ""


# The 14 slash commands (§5.2).
# dispatch="agent": prose → structured prompt → the agent's run_dreadgoad tool
#   (robust arg interpretation; the arg-flexible/mutating commands).
# dispatch="direct": deterministic reads + /destroy, run programmatically.
REGISTRY: dict[str, Command] = {
    "/up": Command(
        "/up",
        ("up",),
        dispatch="agent",
        long_running=True,
        description="Deploy the range end-to-end: doctor → infra → provision → health",
        detail="creates cloud resources and starts billing; runs for tens of minutes",
    ),
    "/provision": Command(
        "/provision",
        ("provision",),
        dispatch="agent",
        long_running=True,
        description="Re-run the Ansible provisioning playbooks, with retries",
        detail="safe to repeat; configures existing hosts, never recreates infra",
    ),
    "/reset": Command(
        "/reset",
        ("lab", "reset"),
        dispatch="agent",
        long_running=True,
        description="Restore Active Directory to a known-clean baseline",
        detail="discards AD changes made since deploy; leaves the VMs in place",
    ),
    "/start": Command(
        "/start",
        ("lab", "start"),
        description="Power on the stopped lab instances",
        detail="resumes compute billing; takes a few minutes to become reachable",
    ),
    "/stop": Command(
        "/stop",
        ("lab", "stop"),
        description="Power off the running lab instances",
        detail="stops compute billing; disks and range state are preserved",
    ),
    "/destroy": Command(
        "/destroy",
        ("infra", "destroy"),
        long_running=True,
        description="Tear down all infrastructure for this environment",
        detail="irreversible — deletes the VMs, disks and network",
    ),
    "/instances": Command(
        "/instances",
        ("lab", "status", "--json"),
        description="Power state and private IP of every VM in the range",
        detail="read-only; also refreshes the range view",
    ),
    "/health": Command(
        "/health",
        ("health-check", "--json"),
        long_running=True,
        description="Check each host is reachable and Active Directory is serving",
        detail="read-only; reports per host, so a failure is scoped to one machine",
    ),
    "/validate": Command(
        "/validate",
        ("validate",),
        long_running=True,
        description="Check the vulnerability configuration matches this variant",
        detail="read-only; needs the variant's mapping.json and an inventory",
    ),
    "/diagnose": Command(
        "/diagnose",
        ("diagnose",),
        long_running=True,
        description="Deep connectivity checks against the domain controllers",
        detail="read-only; use when /health fails and you need the reason",
    ),
    "/score": Command(
        "/score",
        ("score",),
        dispatch="agent",
        takes_args=True,
        description="Score an agent's report against the answer key",
        detail="give it the report path on the attack box; it is fetched for you",
    ),
    "/scrub": Command(
        "/scrub",
        ("score", "reset"),
        takes_args=True,
        description="Clean agent artifacts off the attack box and Windows hosts",
        detail="deletes for real; add 'dry' to preview instead. Leaves AD config alone",
    ),
    "/variant": Command(
        "/variant",
        ("variant", "generate"),
        dispatch="agent",
        takes_args=True,
        description="Generate a randomized-name variant of the base lab",
        detail="new names, passwords and answer key — desyncs a deployed range",
    ),
    "/extensions": Command(
        "/extensions",
        ("extension",),
        dispatch="agent",
        takes_args=True,
        description="List available extensions, or provision one by name",
        detail="listing is read-only; provisioning adds machines to the range",
    ),
}

# Commands the agent may run via its run_dreadgoad tool: ALL of them, so it can
# answer questions by running reads (/instances, /health, …) and perform actions
# from natural language. Safety for destructive commands (/destroy, /up, /reset,
# /variant) is by prompt — the agent must confirm intent (operator's choice).
AGENT_RUNNABLE: frozenset[str] = frozenset(REGISTRY)


def command_catalog() -> list[dict[str, t.Any]]:
    """Registry as a JSON-able list for the frontend autocomplete menu (§5.1).

    Preserves REGISTRY insertion order. ``dispatch`` lets the UI tag each row
    (direct vs agent); ``takes_args`` hints whether free-form args are expected.
    """
    return [
        {
            "name": name,
            "description": c.description,
            "detail": c.detail,
            # The CLI verb it maps to — an operator who knows `dreadgoad` can
            # tell at a glance what will actually run.
            "cli": "dreadgoad " + " ".join(c.verb),
            "dispatch": c.dispatch,
            "long_running": c.long_running,
            "takes_args": c.takes_args,
        }
        for name, c in REGISTRY.items()
    ]


def expand_command_prompt(name: str, extra: list[str]) -> str:
    """Turn a ``dispatch="agent"`` command into a structured prompt (ALFRED-style).

    The agent interprets the operator's free-form args into flags and runs the
    command via ``run_dreadgoad`` — constrained to this one command, this range.
    A ``prompts/<command>.md`` file, if present, is injected as command-specific
    guidance (flag semantics, gotchas); otherwise the generic template stands.
    """
    cmd = REGISTRY[name]
    verb = " ".join(cmd.verb)
    freeform = " ".join(extra) if extra else "(no extra arguments given)"
    guidance = load_prompt(name.lstrip("/"))
    guidance_block = f"\n\n## Command-specific guidance\n{guidance}" if guidance else ""
    return (
        f"The operator invoked the {name} command — {cmd.description}.\n\n"
        f"Run it using the `run_dreadgoad` tool with command={name!r}. Do NOT use "
        f"any other command, and NEVER use raw cloud CLI (aws/az/terraform) — only "
        f"`run_dreadgoad`. The range (config/env) is fixed by the tool; don't pass "
        f"--config/--env.\n\n"
        f"Interpret the operator's free-form request into the correct dreadgoad "
        f"flags for `{verb}` and pass them as the tool's `args`. If the request is "
        f"ambiguous or would be destructive beyond the command's intent, ask first."
        f"{guidance_block}\n\n"
        f"Operator's request: {name} {freeform}"
    )


def resolve_bin(repo_root: str | Path) -> str:
    """Locate the dreadgoad binary.

    Prefer the repo's freshly-built ``cli/dreadgoad`` — it's the version built
    alongside the console and has its ``--json`` verbs — over whatever
    ``dreadgoad`` happens to be on PATH (which could be stale and lack them).
    """
    repo_bin = Path(repo_root) / "cli" / "dreadgoad"
    if repo_bin.is_file() and os.access(repo_bin, os.X_OK):
        return str(repo_bin)
    found = shutil.which("dreadgoad")
    if found:
        return found
    return str(repo_bin)  # expected path; yields a clear error if missing


# Tokens that turn /scrub into a preview. The CLI's own flag is `--apply`, but
# the console inverts the default (see below), so the operator needs a word for
# the *other* mode — accept the obvious spellings rather than one exact string.
_SCRUB_DRY_TOKENS = frozenset(
    {"dry", "dry-run", "dryrun", "--dry", "--dry-run", "--dryrun", "-n", "preview"}
)

# `score reset` flags that consume the following token. Without this, a value
# that happens to spell a dry token (``--report-output dry``) would be eaten as
# the mode — dropping --apply *and* leaving the flag with no value.
_SCRUB_VALUE_FLAGS = frozenset(
    {"--attack-box", "--ssh-key", "--ssh-user", "--report-output"}
)


def _verb_for(cmd: Command, extra: list[str]) -> tuple[list[str], list[str]]:
    """Resolve a command's concrete verb + trailing args from chat args.

    Handles the arg-shaped commands:
      - /extensions  → `extension list` (no arg) or `extension provision <name>`
      - /score       → `score --report <path>` (+ any flags like --live-verify)
      - /variant     → `variant generate <flags…>`
      - /scrub       → `score reset --apply` unless a dry token is given
    """
    if cmd.name == "/scrub":
        # The CLI defaults to a dry run; the console defaults to applying.
        # Someone typing "clean the box" means clean it — a command that
        # silently changed nothing was the more surprising behaviour. Any other
        # argument (--purge-ad, --skip-kali, …) passes straight through.
        dry = False
        rest: list[str] = []
        expect_value = False
        for arg in extra:
            if expect_value:  # this token belongs to the preceding flag
                rest.append(arg)
                expect_value = False
                continue
            lowered = arg.lower()
            if lowered in _SCRUB_VALUE_FLAGS:
                rest.append(arg)
                expect_value = True
                continue
            if lowered in _SCRUB_DRY_TOKENS:
                dry = True
                continue
            rest.append(arg)
        verb = ["score", "reset"] if dry else ["score", "reset", "--apply"]
        return verb, rest
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
        "--config",
        str(anchor["config_path"]),
        "--env",
        str(anchor["env"]),
        *verb,
        *trailing,
    ]


def is_command(text: str) -> bool:
    """True if the message's first token is a registered slash command."""
    return text.strip().split(" ", 1)[0] in REGISTRY


def parse_command(text: str) -> tuple[str, list[str]]:
    """Split ``/cmd arg1 "arg with spaces"`` → ("/cmd", ["arg1", "arg with spaces"]).

    Uses shell-style tokenization so quoted args (e.g. paths with spaces)
    survive; falls back to plain split on malformed quoting. Only called after
    ``is_command`` confirms a leading command token, so ``parts`` is non-empty.
    """
    text = text.strip()
    try:
        parts = shlex.split(text)
    except ValueError:
        parts = text.split()
    return parts[0], parts[1:]
