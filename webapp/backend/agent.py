"""Per-session dreadgoad agent factory (design §5).

Adapted from ALFRED's agent: a ``LocalTaskAgent`` that bypasses platform
telemetry, sandboxes filesystem writes to the session working dir, and is
told how to drive the dreadgoad CLI for *this* session's range (its
``(config_path, env)`` anchor). Free-text prompts go here; deterministic
slash commands are dispatched directly (see server WS handler).
"""

from __future__ import annotations

import string
import typing as t
from contextlib import AsyncExitStack, aclosing, asynccontextmanager
from copy import deepcopy

import rigging as rg
from dreadnode.agent import TaskAgent
from dreadnode.agent.agent import CommitBehavior
from dreadnode.agent.events import AgentEvent
from dreadnode.agent.thread import Thread
from dreadnode.agent.tools import tool
from dreadnode.agent.tools.fs import Filesystem

from . import commands

# Signature of the shared command pipeline (chat.run_cli), injected to avoid a
# chat <-> agent import cycle: (app, session_id, command, args) -> (exit, output).
RunCli = t.Callable[[t.Any, str, str, list[str]], t.Awaitable[tuple[int, str]]]


class LocalTaskAgent(TaskAgent):
    """TaskAgent that streams without platform telemetry (ALFRED pattern)."""

    _REMOVE_TOOLS = {"finish_task", "give_up_on_task", "update_todo"}

    def model_post_init(self, context: t.Any) -> None:
        super().model_post_init(context)
        self.tools = [
            tool for tool in self.tools if tool.name not in self._REMOVE_TOOLS
        ]
        self.stop_conditions = [
            c for c in self.stop_conditions if c.name != "stop_never"
        ]

    @asynccontextmanager
    async def stream(
        self,
        user_input: str,
        *,
        thread: Thread | None = None,
        commit: CommitBehavior = "always",
    ) -> t.AsyncIterator[t.AsyncGenerator[AgentEvent, None]]:
        thread = thread or self.thread
        messages = [*deepcopy(thread.messages), rg.Message("user", str(user_input))]
        async with AsyncExitStack() as stack:
            for tool_container in self.tools:
                if hasattr(tool_container, "__aenter__") and hasattr(
                    tool_container, "__aexit__"
                ):
                    # Toolset satisfies the async-CM protocol at runtime (guarded
                    # above); its wrapped dunders confuse pyright's protocol check.
                    await stack.enter_async_context(tool_container)  # type: ignore[arg-type]
            async with aclosing(
                self._stream(thread, messages, commit=commit)
            ) as events:
                yield events


# Minimal fallback if prompts/system.md is somehow missing (packaging bug) — the
# agent should never run with empty instructions.
_SYSTEM_FALLBACK = (
    "You are the DreadGOAD range agent. Operate on THIS range only via the "
    "`run_dreadgoad` tool (config/env are injected). Never run raw cloud CLI "
    "(aws/az/terraform) or arbitrary shell. Confirm ambiguous destructive ops."
)


def _instructions(session: dict[str, t.Any]) -> str:
    """Render the shared system prompt from ``prompts/system.md``.

    The template uses ``$placeholder`` fields filled from the session's anchor
    and snapshot. Falls back to a terse inline prompt if the file is missing.
    """
    anchor = session["anchor"]
    snap = session.get("snapshot", {})
    template = commands.load_prompt("system")
    if template is None:
        return _SYSTEM_FALLBACK
    return string.Template(template).safe_substitute(
        config_path=anchor["config_path"],
        env=anchor["env"],
        provider=snap.get("provider"),
        lab=snap.get("lab"),
    )


def _make_run_dreadgoad(app: t.Any, session_id: str, run_cli: RunCli):  # noqa: ANN202
    """Build the session-bound run_dreadgoad tool.

    The agent may run ANY registered dreadgoad command (validated against
    ``commands.AGENT_RUNNABLE``) — reads to answer questions, actions to perform
    them. Everything routes through the shared pipeline so agent-initiated ops get
    streaming/status/hook/cancel like operator-typed ones. Guardrails for
    destructive commands are by prompt (the agent confirms intent).
    """

    @tool(catch=True)
    async def run_dreadgoad(command: str, args: list[str] | None = None) -> str:
        """Run a dreadgoad command for THIS session and return its result.

        Use reads (/instances, /health, /validate, /diagnose) to answer questions,
        and the action commands to perform what the operator asked.

        Args:
            command: a dreadgoad slash command — reads (/instances, /health,
                /validate, /diagnose, /start, /stop, /scrub) or actions (/up,
                /provision, /reset, /variant, /extensions, /score, /destroy).
            args: CLI flags/values interpreted from the operator's request, e.g.
                ["--from", "ad-data.yml"] or ["/remote/report.jsonl", "--live-verify"].
                Do NOT pass --config/--env — the range is fixed.

        Returns a short summary (exit status + output tail).
        """
        if command not in commands.AGENT_RUNNABLE:
            return (
                f"Refused: {command!r} is not a known dreadgoad command. "
                f"Valid commands: {sorted(commands.AGENT_RUNNABLE)}."
            )
        exit_code, output = await run_cli(app, session_id, command, list(args or []))
        status = "succeeded" if exit_code == 0 else f"failed (exit {exit_code})"
        return f"`dreadgoad {command}` {status}.\n{output[-1500:]}"

    return run_dreadgoad


def create_agent(
    model: str,
    session: dict[str, t.Any],
    app: t.Any,
    session_id: str,
    run_cli: RunCli,
) -> TaskAgent:
    """Build a configured agent for a session.

    The LLM key must be in the environment (e.g. OPENROUTER_API_KEY). The
    default model is Sonnet 5 via OpenRouter (see server config). The agent's
    only range-mutating tool is ``run_dreadgoad`` (constrained to this session);
    file writes are sandboxed to the session dir. No general shell tool.
    """
    session_dir = session.get("session_dir", ".")
    fs = Filesystem(path=session_dir, variant="write")
    return LocalTaskAgent(
        name="dreadgoad-agent",
        description="Builds, manages, and validates a DreadGOAD range",
        model=model,
        instructions=_instructions(session),
        max_steps=50,
        tools=[fs, _make_run_dreadgoad(app, session_id, run_cli)],
    )
