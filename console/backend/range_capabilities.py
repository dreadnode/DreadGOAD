"""Load the selected range's semantic command capabilities from the CLI."""

from __future__ import annotations

import json
import typing as t
from dataclasses import dataclass

from . import commands, projectroot
from .cli import Capture, capture


class Capability(t.TypedDict):
    name: str
    supported: bool
    description: t.NotRequired[str]
    detail: t.NotRequired[str]
    protocol: t.NotRequired[str]
    handler_type: t.NotRequired[str]
    profile: t.NotRequired[str]


MAX_AGENT_PROMPT_CHARS = 32 * 1024


@dataclass(frozen=True)
class RangeContext(t.Mapping[str, Capability]):
    """Range-owned model context plus its authoritative command mapping."""

    commands: dict[str, Capability]
    agent_prompt: str | None = None

    def __getitem__(self, key: str) -> Capability:
        return self.commands[key]

    def __iter__(self) -> t.Iterator[str]:
        return iter(self.commands)

    def __len__(self) -> int:
        return len(self.commands)


async def load(
    session: t.Mapping[str, t.Any],
    fallback_root: str,
    capture_command: Capture | None = None,
) -> RangeContext:
    """Return validated capabilities keyed by slash-command name.

    Failure is explicit rather than silently falling back to the global
    catalog: showing a command that belongs to another range is worse than a
    temporarily unavailable catalog.
    """
    anchor = session.get("anchor") or {}
    config_path, env = anchor.get("config_path"), anchor.get("env")
    if not config_path or not env:
        raise ValueError("session has no config/environment anchor")
    root, _ = projectroot.resolve_root(str(config_path))
    argv = [
        commands.resolve_bin(fallback_root),
        "--config",
        str(config_path),
        "--env",
        str(env),
        "range",
        "capabilities",
    ]
    runner = capture_command or capture
    rc, stdout, stderr = await runner(argv, str(root))
    if rc != 0:
        raise ValueError((stderr or stdout or f"capabilities exited {rc}").strip())
    try:
        payload = json.loads(stdout)
    except (TypeError, ValueError) as exc:
        raise ValueError("range capabilities returned invalid JSON") from exc
    rows = payload.get("commands") if isinstance(payload, dict) else None
    if not isinstance(rows, list):
        raise ValueError("range capabilities response has no commands list")

    result: dict[str, Capability] = {}
    for raw in rows:
        if not isinstance(raw, dict):
            raise ValueError("range capability must be an object")
        name, supported = raw.get("name"), raw.get("supported")
        if not isinstance(name, str) or not isinstance(supported, bool):
            raise ValueError(
                "range capability requires string name and boolean supported"
            )
        slash_name = f"/{name}"
        if slash_name not in commands.REGISTRY:
            raise ValueError(f"range reported unknown capability {name!r}")
        capability: Capability = {"name": name, "supported": supported}
        for key in ("description", "detail", "protocol", "handler_type", "profile"):
            value = raw.get(key)
            if value is not None:
                if not isinstance(value, str):
                    raise ValueError(f"range capability {name!r} has invalid {key}")
                t.cast(dict[str, t.Any], capability)[key] = value
        if slash_name in result:
            raise ValueError(f"range reported duplicate capability {name!r}")
        result[slash_name] = capability
    expected = {"/health", "/validate", "/score", "/reset", "/scrub"}
    missing = expected.difference(result)
    if missing:
        raise ValueError(
            "range capabilities response omitted: " + ", ".join(sorted(missing))
        )
    raw_prompt = payload.get("agent_prompt") if isinstance(payload, dict) else None
    if raw_prompt is not None:
        if not isinstance(raw_prompt, str):
            raise ValueError("range agent_prompt must be a string")
        if not raw_prompt.strip():
            raise ValueError("range agent_prompt must not be empty")
        if len(raw_prompt) > MAX_AGENT_PROMPT_CHARS:
            raise ValueError(
                f"range agent_prompt exceeds {MAX_AGENT_PROMPT_CHARS} characters"
            )
        raw_prompt = raw_prompt.strip()
    return RangeContext(result, raw_prompt)
