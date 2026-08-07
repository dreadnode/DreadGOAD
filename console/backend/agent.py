"""Per-session dreadgoad agent factory (design §5).

Adapted from ALFRED's agent: a ``LocalTaskAgent`` that bypasses platform
telemetry, sandboxes filesystem writes to the session working dir, and is
told how to drive the dreadgoad CLI for *this* session's range (its
``(config_path, env)`` anchor). Free-text prompts go here; deterministic
slash commands are dispatched directly (see server WS handler).
"""

from __future__ import annotations

import asyncio
import string
import typing as t
from contextlib import AsyncExitStack, aclosing, asynccontextmanager
from copy import deepcopy

import rigging as rg
from rigging.error import Stop
from dreadnode.agent import TaskAgent
from dreadnode.agent.agent import CommitBehavior
from dreadnode.agent.events import AgentEvent
from dreadnode.agent.thread import Thread
from dreadnode.agent.tools import tool
from dreadnode.agent.tools.fs import Filesystem

from . import commands, summary

# Signature of the shared command pipeline (chat.run_cli), injected to avoid a
# chat <-> agent import cycle: (app, session_id, command, args) -> (exit, output).
RunCli = t.Callable[[t.Any, str, str, list[str]], t.Awaitable[tuple[int, str]]]


class LocalTaskAgent(TaskAgent):
    """TaskAgent that streams without platform telemetry (ALFRED pattern)."""

    _REMOVE_TOOLS = {"finish_task", "give_up_on_task", "update_todo"}

    def model_post_init(self, context: t.Any) -> None:
        """Strip the task-lifecycle tools and the never-stop condition.

        This agent runs one operator turn at a time rather than a self-directed
        task, so ``finish_task``/``give_up_on_task``/``update_todo`` and
        ``stop_never`` would let it loop instead of answering.
        """
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
        """Stream one turn's events, bypassing platform telemetry.

        Yields the event generator as a context manager so toolsets are entered
        and closed around the run. Args: ``user_input`` the prompt; ``thread``
        an alternate conversation (defaults to the agent's own); ``commit`` how
        messages are written back to it.
        """
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

    Only config-derived snapshot fields belong here. Instructions are rendered
    once, when the agent is first created and cached (see chat._get_agent), so a
    field the ingestion hook learns post-deploy — ``account``, ``group``,
    ``attack_box`` — would freeze at whatever it was on the first turn, usually
    empty. Those stay out; the agent reads them from ``/instances``, which is
    always current.

    Editing ``system.md``: every ``$name`` in it is a substitution, so a literal
    dollar sign must be written ``$$``. This bites hardest on PowerShell — a
    ``/exec`` example containing ``$env:COMPUTERNAME`` silently renders as
    ``dreadindex:COMPUTERNAME``, because ``env`` is one of the keys below. The
    corruption leaves no ``$`` behind, so the "no unsubstituted placeholder"
    test in test_commands.py cannot catch it.
    """
    anchor = session["anchor"]
    snap = session.get("snapshot", {})
    template = commands.load_prompt("system")
    if template is None:
        return _SYSTEM_FALLBACK

    def field(value: t.Any) -> str:
        """Render one snapshot value, or ``(not set)`` when it's absent."""
        # A bare None would render as the string "None" and read to the model as
        # a real value; say plainly that it isn't set.
        text = str(value).strip() if value is not None else ""
        return text or "(not set)"

    return string.Template(template).safe_substitute(
        config_path=anchor["config_path"],
        env=anchor["env"],
        provider=field(snap.get("provider")),
        lab=field(snap.get("lab")),
        region=field(snap.get("region")),
        variant_name=field(snap.get("variant_name")),
        vpc_cidr=field(snap.get("vpc_cidr")),
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

        Use reads (/instances, /health, /validate) to answer questions, and the
        action commands to perform what the operator asked.

        Args:
            command: a dreadgoad slash command — reads (/instances, /health,
                /validate) or actions, which change the range (/start, /stop,
                /up, /provision, /reset, /scrub, /exec, /variant, /extensions,
                /score, /destroy). /scrub deletes by default; pass "dry" to
                preview. /exec runs an arbitrary admin-level script on named
                hosts and has no dry run.
            args: CLI flags/values interpreted from the operator's request, e.g.
                ["--from", "ad-data.yml"] or ["/remote/report.jsonl", "--live-verify"].
                Do NOT pass --config/--env — the range is fixed.

        Returns the exit status plus the command's output, condensed: reads are
        rendered as compact per-record lines, long logs are clipped in the
        middle with a marker stating how many lines were dropped.
        """
        if command not in commands.AGENT_RUNNABLE:
            return (
                f"Refused: {command!r} is not a known dreadgoad command. "
                f"Valid commands: {sorted(commands.AGENT_RUNNABLE)}."
            )
        # An operator cancel reaches us as CancelledError, raised deliberately by
        # run_cli as a signal (command_runner.py). Letting it escape a tool call
        # is what produced the "tool_use ids were found without tool_result"
        # 400s: CancelledError is a BaseException, so every layer above catches
        # only Exception and none of them see it —
        #
        #   rigging @tool(catch=True)  except Exception   (tools/base.py)
        #   _process_tool_call         except Exception   (agent/agent.py)
        #   join_generators            except Exception   (util.py)
        #
        # The last one has a `finally` that queues its FINISHED sentinel, so the
        # join loop ends *normally* with no ToolEnd. The tool_use block is
        # already in the message list, its tool_result never arrives, and the
        # agent then makes another generation call against an unpaired list —
        # rejected by every provider, and the turn dies with a confusing 400
        # instead of reading as a cancel.
        #
        # Stop is rigging's own way for a tool to end a run: it is caught by
        # name in handle_tool_call, so a real tool_result IS appended and the
        # agent raises Finish instead of generating again. The pair stays
        # balanced and the run ends immediately — which is also the behaviour
        # cancelling is supposed to have, no chance for the agent to retry.
        try:
            exit_code, output = await run_cli(
                app, session_id, command, list(args or [])
            )
        except asyncio.CancelledError:
            # Only convert the signal. A genuine teardown (task.cancel(), e.g.
            # cleanup_session on shutdown) must never be swallowed, and
            # cancelling() is the one thing that tells them apart: it counts
            # cancel() calls against THIS task, so it is 0 for run_cli's raise
            # and non-zero only when someone really cancelled us.
            #
            # getattr because cancelling() is 3.11+ and the console documents
            # 3.10 (console/README.md). Calling it bare there would raise an
            # AttributeError *from inside this handler*, replacing the cancel
            # with a crash. Where it is unavailable the signal is the far more
            # common case, so treat it as one: a genuine cancel then ends the
            # turn through Finish instead of CancelledError, which still stops
            # the run immediately rather than letting it continue.
            current = asyncio.current_task()
            cancelling = getattr(current, "cancelling", None)
            if cancelling is not None and cancelling() > 0:
                raise
            raise Stop(
                f"The operator cancelled `dreadgoad {command}`. "
                "Stopping this turn; do not retry the command."
            ) from None
        # Distinguishes a cancel from a failure: a negative code means the run
        # was signalled, and calling that "failed" made the model report a
        # deliberate stop as an error (see summary.describe_exit).
        status = summary.describe_exit(exit_code)
        # Structured where possible, clipped-with-a-marker otherwise. Never a
        # bare tail: that silently drops records and the model reports the
        # fragment as the whole (see summary.py).
        return f"`dreadgoad {command}` {status}.\n{summary.summarize(command, output)}"

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
