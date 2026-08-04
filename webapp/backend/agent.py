"""Per-session dreadgoad agent factory (design §5).

Adapted from ALFRED's agent: a ``LocalTaskAgent`` that bypasses platform
telemetry, sandboxes filesystem writes to the session working dir, and is
told how to drive the dreadgoad CLI for *this* session's range (its
``(config_path, env)`` anchor). Free-text prompts go here; deterministic
slash commands are dispatched directly (see server WS handler).
"""

from __future__ import annotations

import typing as t
from contextlib import AsyncExitStack, aclosing, asynccontextmanager
from copy import deepcopy

import rigging as rg
from dreadnode.agent import TaskAgent
from dreadnode.agent.agent import CommitBehavior
from dreadnode.agent.events import AgentEvent
from dreadnode.agent.thread import Thread
from dreadnode.agent.tools.execute import command
from dreadnode.agent.tools.fs import Filesystem


class LocalTaskAgent(TaskAgent):
    """TaskAgent that streams without platform telemetry (ALFRED pattern)."""

    _REMOVE_TOOLS = {"finish_task", "give_up_on_task", "update_todo"}

    def model_post_init(self, context: t.Any) -> None:
        super().model_post_init(context)
        self.tools = [tool for tool in self.tools if tool.name not in self._REMOVE_TOOLS]
        self.stop_conditions = [c for c in self.stop_conditions if c.name != "stop_never"]

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
                if hasattr(tool_container, "__aenter__") and hasattr(tool_container, "__aexit__"):
                    await stack.enter_async_context(tool_container)
            async with aclosing(self._stream(thread, messages, commit=commit)) as events:
                yield events


def _instructions(session: dict[str, t.Any], repo_root: str) -> str:
    anchor = session["anchor"]
    snap = session.get("snapshot", {})
    return f"""\
You are the DreadGOAD range agent. You help build, manage, reset, and validate
one Active Directory lab range via the `dreadgoad` CLI.

## This session's range
- Config file: {anchor['config_path']}
- Environment: {anchor['env']}
- Provider: {snap.get('provider')}   Lab/variant: {snap.get('lab')}

## Running the CLI
Always target THIS range by passing the anchor flags:
    dreadgoad --config {anchor['config_path']} --env {anchor['env']} <command>
Run CLI commands from the repo root ({repo_root}); the CLI reads ad/, infra/,
and dreadgoad.yaml from there. Provider is set in the config file — never pass
--provider.

## Rules
- Your file workspace is the session directory; keep notes/artifacts there.
- Prefer the dedicated slash commands the operator has for common operations.
- Destructive actions (destroy, reset) change real cloud state — confirm intent.
- Report what you ran and the result concisely.
"""


def create_agent(model: str, session: dict[str, t.Any], repo_root: str) -> TaskAgent:
    """Build a configured agent for a session.

    The LLM key must be in the environment (e.g. OPENROUTER_API_KEY). The
    default model is Sonnet 5 via OpenRouter (see server config).
    """
    session_dir = session.get("session_dir", ".")
    fs = Filesystem(path=session_dir, variant="write")
    return LocalTaskAgent(
        name="dreadgoad-agent",
        description="Builds, manages, and validates a DreadGOAD range",
        model=model,
        instructions=_instructions(session, repo_root),
        max_steps=50,
        tools=[command, fs],
    )
