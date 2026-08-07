"""Multiplexed chat WebSocket (design §5.1, §7).

One socket carries a ``session_id`` on every message. Dispatch (§5.1):
  - ``dispatch="direct"`` commands (deterministic reads, /destroy) run the CLI
    programmatically via ``run_cli``;
  - ``dispatch="agent"`` commands expand to a structured prompt and run through
    the agent's ``run_dreadgoad`` tool — which calls the *same* ``run_cli``, so
    both paths stream/status/hook/cancel identically;
  - free-text goes to the agent.
All events are persisted to the event log and replayed on resume.

Live behavior needs an LLM key (OPENROUTER_API_KEY); the structural wiring is
import-verifiable without one.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import typing as t
from copy import deepcopy
from dataclasses import dataclass, field
from datetime import datetime, timezone

from dreadnode.agent.events import (
    AgentEnd,
    AgentError,
    GenerationEnd,
    ToolEnd,
    ToolStart,
)

from . import commands, fetch, hook, paths, summary
from .agent import create_agent
from .cli import start_capture, start_command

# Chat-kind events replayed on resume (§6.3).
CHAT_KINDS = [
    "user_message",
    "generation",
    "tool_start",
    "tool_end",
    "error",
    "agent_end",
    "status",
    # Formatted report panels. They render from persisted state, so without
    # these a reload silently drops the /instances and /health tables that were
    # visible a moment earlier. ``command_progress`` is deliberately absent —
    # it's live-only and never persisted; ``check_run`` too, since RangeView
    # re-fetches the range over REST rather than replaying it.
    "instances_report",
    "health_report",
    "validate_report",
    "scrub_report",
    "exec_report",
]

# Strong refs to in-flight turn tasks so they aren't GC'd and survive a client
# disconnect (the op keeps running server-side, §5.4).
_tasks: set[asyncio.Task[t.Any]] = set()


@dataclass(slots=True)
class TurnState:
    """Typed ownership state for one admitted chat turn."""

    started_at: str = field(
        default_factory=lambda: datetime.now(timezone.utc).isoformat()
    )
    command: str | None = None
    cancelled: bool = False
    started: bool = False
    commands_starting: int = 0
    task: asyncio.Task[t.Any] | None = None


@dataclass(slots=True)
class SessionRuntime:
    """Every in-memory resource owned by one console session."""

    agent: t.Any = None
    lock: asyncio.Lock = field(default_factory=asyncio.Lock)
    conn: t.Any = None
    turn: TurnState | None = None
    running: set[t.Any] = field(default_factory=set)
    # Kept true after deletion so stale WebSockets cannot recreate orphan data.
    closing: bool = False


_runtimes: dict[str, SessionRuntime] = {}


def _runtime(session_id: str) -> SessionRuntime:
    runtime = _runtimes.get(session_id)
    if runtime is None:
        runtime = SessionRuntime()
        _runtimes[session_id] = runtime
    return runtime


def _discard_idle_runtime(session_id: str, runtime: SessionRuntime) -> None:
    """Drop lock-only shells while retaining agents and deletion tombstones."""
    if (
        runtime.agent is None
        and runtime.conn is None
        and runtime.turn is None
        and not runtime.running
        and not runtime.closing
        and _runtimes.get(session_id) is runtime
    ):
        _runtimes.pop(session_id, None)

TURN_BUSY_MESSAGE = "A turn is already running for this session; wait or cancel it first."


def active_turn(session_id: str) -> TurnState | None:
    """The running turn for a session, or None if it's idle."""
    runtime = _runtimes.get(session_id)
    return runtime.turn if runtime is not None else None


def begin_cleanup(session_id: str) -> bool:
    """Atomically reserve an idle session against new turn dispatch."""
    runtime = _runtime(session_id)
    if runtime.closing or runtime.turn is not None or runtime.running:
        return False
    runtime.closing = True
    return True


def release_cleanup(session_id: str) -> None:
    """Release a failed deletion reservation so the session remains usable."""
    runtime = _runtimes.get(session_id)
    if runtime is not None:
        runtime.closing = False
        _discard_idle_runtime(session_id, runtime)


def session_closing(session_id: str) -> bool:
    runtime = _runtimes.get(session_id)
    return runtime.closing if runtime is not None else False


def register_conn(session_id: str, ws: t.Any) -> None:
    """Mark ``ws`` as the current socket for a session (called per message)."""
    _runtime(session_id).conn = ws


def unregister_conn(ws: t.Any) -> None:
    """Drop a closed socket from the registry (best-effort)."""
    for session_id, runtime in list(_runtimes.items()):
        if runtime.conn is ws:
            runtime.conn = None
            _discard_idle_runtime(session_id, runtime)


def cancel_session(session_id: str) -> bool:
    """Cancel the whole in-flight turn and every CLI process it owns."""
    runtime = _runtimes.get(session_id)
    if runtime is None:
        return False
    turn = runtime.turn
    running = tuple(runtime.running)
    if turn is None and not running:
        return False

    if turn is not None:
        turn.cancelled = True
    for rc in running:
        rc.cancel()

    # With no CLI to unwind, interrupt model generation immediately. A running
    # CLI is allowed to handle SIGINT first; run_cli then aborts the owning turn
    # before the agent can interpret the cancellation as a failure and retry.
    if (
        not running
        and turn is not None
        and not turn.commands_starting
    ):
        task = turn.task
        if task is not None and not task.done():
            if turn.started:
                task.cancel()
            # A task cancelled before its coroutine first runs cannot emit the
            # terminal event. An unstarted _runner observes the flag itself.
    return True


def _session_lock(session_id: str) -> asyncio.Lock:
    return _runtime(session_id).lock


def dispatch(
    app: t.Any, session_id: str, content: str
) -> asyncio.Task[t.Any] | None:
    """Start one background turn, or reject it if the session is busy (§6.4, §7).

    The WS recv loop calls this and immediately keeps reading, so `cancel` and
    other sessions' messages are handled while a long op streams (§5.4).
    Admission is reserved synchronously, before the task can yield, so two rapid
    messages cannot both slip past the check and queue behind the session lock.
    Tasks are kept in ``_tasks`` so they survive the connection closing; emits
    target the session's *current* socket (`SessionRuntime.conn`).
    """

    runtime = _runtime(session_id)
    if runtime.turn is not None or runtime.closing:
        return None

    turn = TurnState()
    runtime.turn = turn

    async def _runner() -> None:
        try:
            turn.started = True
            if turn.cancelled:
                raise asyncio.CancelledError
            async with runtime.lock:
                await handle_message(app, session_id, content)
        except asyncio.CancelledError:
            # One terminal event releases the UI's processing state whether the
            # cancel landed during model generation or a CLI tool call.
            await emit_event(
                app,
                session_id,
                "agent_end",
                {"failed": False, "cancelled": True},
            )
            raise
        finally:
            # Identity check protects a newer reservation if cleanup removed
            # this one while its task was still unwinding.
            if runtime.turn is turn:
                runtime.turn = None

    try:
        task = asyncio.create_task(_runner())
    except Exception:
        if runtime.turn is turn:
            runtime.turn = None
        raise
    turn.task = task
    _tasks.add(task)
    task.add_done_callback(
        lambda finished: _turn_task_done(session_id, turn, finished)
    )
    return task


def _turn_task_done(
    session_id: str, turn: TurnState, task: asyncio.Task[t.Any]
) -> None:
    """Release a finished turn and observe any exception it carried.

    ``dispatch`` deliberately runs turns in the background, so most tasks are
    never awaited by a caller. Retrieving the exception here prevents an
    unexpected pipeline failure from becoming an unobserved "Task exception was
    never retrieved" warning; focused tests may still await the task and receive
    the same exception normally.
    """
    _tasks.discard(task)
    # A task cancelled before its coroutine first runs never enters the
    # coroutine's ``finally`` block, so release its admission here as well.
    runtime = _runtimes.get(session_id)
    if runtime is not None and runtime.turn is turn:
        runtime.turn = None
    if runtime is not None:
        _discard_idle_runtime(session_id, runtime)
    if not task.cancelled():
        task.exception()


async def cleanup_session(session_id: str, *, timeout: float = 15.0) -> None:
    """Stop and await one session, then evict all of its runtime state."""
    runtime = _runtimes.get(session_id)
    if runtime is None:
        return
    turn = runtime.turn
    running = tuple(runtime.running)
    cancel_session(session_id)

    task = turn.task if turn is not None else None
    if task is not None and not task.done():
        done, _ = await asyncio.wait({task}, timeout=timeout)
        if not done:
            for rc in tuple(runtime.running):
                force_kill = getattr(rc, "force_kill", None)
                if force_kill is not None:
                    force_kill()
                else:
                    rc.cancel()
            task.cancel()
            with contextlib.suppress(asyncio.CancelledError, Exception):
                await task
    elif running:
        # No owner task can reap these handles; do not leave them detached.
        for rc in running:
            force_kill = getattr(rc, "force_kill", None)
            if force_kill is not None:
                force_kill()

    runtime.agent = None
    runtime.conn = None
    runtime.turn = None
    runtime.running.clear()
    if not runtime.closing:
        _runtimes.pop(session_id, None)


async def cleanup_all(*, timeout: float = 15.0) -> None:
    """Bounded shutdown cleanup for every in-memory session."""
    session_ids = set(_runtimes)
    for runtime in _runtimes.values():
        runtime.closing = True
    await asyncio.gather(
        *(cleanup_session(session_id, timeout=timeout) for session_id in session_ids)
    )


def format_event(event: t.Any) -> dict[str, t.Any] | None:
    """Convert a dreadnode AgentEvent to a JSON-able chat event (ALFRED shape)."""
    if isinstance(event, GenerationEnd):
        usage = None
        if event.usage:
            usage = {
                "input_tokens": event.usage.input_tokens,
                "output_tokens": event.usage.output_tokens,
            }
        return {
            "kind": "generation",
            "content": event.message.content or "",
            "usage": usage,
        }
    if isinstance(event, ToolStart):
        return {
            "kind": "tool_start",
            "tool": event.tool_call.name,
            "args": event.tool_call.function.arguments,
        }
    if isinstance(event, ToolEnd):
        return {
            "kind": "tool_end",
            "tool": event.tool_call.name,
            "result": (event.message.content or "")[:2000],
        }
    if isinstance(event, AgentError):
        return {"kind": "error", "message": str(event.error)}
    if isinstance(event, AgentEnd):
        return {"kind": "agent_end", "failed": event.result.failed}
    return None


async def _get_agent(app: t.Any, session_id: str) -> t.Any | None:
    runtime = _runtime(session_id)
    if runtime.agent is not None:
        return runtime.agent
    session = await app.state.db.get_session(session_id)
    if session is None:
        return None
    agent = create_agent(
        session.get("model") or "openrouter/anthropic/claude-sonnet-5",
        session,
        app,
        session_id,
        run_cli,
    )
    runtime.agent = agent
    return agent


async def swap_model(
    app: t.Any, session_id: str, new_model: str
) -> dict[str, t.Any] | None:
    """Switch a session's agent model, preserving conversation context (ALFRED-style).

    Runs under the session lock so it can't race an in-flight turn. Persists the
    new model on the session; if an agent is already live, rebuilds it with the
    new model and grafts the old thread's messages onto it so the conversation
    continues seamlessly. Returns the updated session, or None if not found.
    """
    async with _session_lock(session_id):
        session = await app.state.db.get_session(session_id)
        if session is None:
            return None
        session["model"] = new_model
        await app.state.db.upsert_session(session)

        runtime = _runtime(session_id)
        old = runtime.agent
        if old is not None:
            history = deepcopy(old.thread.messages)
            fresh = create_agent(new_model, session, app, session_id, run_cli)
            fresh.thread.messages = history
            runtime.agent = fresh
        # else: no live agent — _get_agent will build with the new model next turn.

    await emit_event(
        app, session_id, "status", {"content": f"Model changed to {new_model}."}
    )
    return session


async def emit_event(
    app: t.Any,
    session_id: str,
    kind: str,
    payload: dict[str, t.Any],
    *,
    persist: bool = True,
) -> None:
    """Persist (optionally) + push an event to the session's current socket (§5.4)."""
    if persist:
        await app.state.db.append_event(session_id, kind, payload)
    runtime = _runtimes.get(session_id)
    ws = runtime.conn if runtime is not None else None
    if ws is None:
        return  # no live client; persisted events replay on reconnect
    try:
        await ws.send_text(
            json.dumps({"session_id": session_id, "kind": kind, **payload})
        )
    except Exception:  # noqa: BLE001
        pass  # client dropped mid-send; op continues, events persisted


# Commands that mutate infra via terraform/ansible — a hard SIGKILL mid-run can
# strand a terraform state lock or half-apply, so they get a long SIGINT runway.
_SLOW_CANCEL = frozenset({"/up", "/provision", "/reset", "/destroy", "/extensions"})


def parse_instances(output: str) -> list[dict[str, t.Any]] | None:
    """Parse the JSON array emitted by ``lab status --json`` (/instances).

    Thin alias over ``summary.parse_json_array`` — the agent's tool result and
    this overlay must read the same output identically, so they share one parser.
    """
    return summary.parse_json_array(output)


def _health_progress(line: str) -> str | None:
    """Human progress string for a per-check ``health-check --json`` NDJSON line.

    Returns None for the final report line (has ``checks``) and any non-check
    noise (e.g. requireInfra's "credentials OK") — those aren't shown live.
    """
    line = line.strip()
    if not line.startswith("{") or '"status"' not in line or '"checks"' in line:
        return None
    try:
        c = json.loads(line)
    except (ValueError, TypeError):
        return None
    if not isinstance(c, dict) or "checks" in c:
        return None
    status, nm, detail = c.get("status", "?"), c.get("name", ""), c.get("detail", "")
    return f"{status:<5} {nm}" + (f" — {detail}" if detail else "")


class _Aborted(Exception):
    """A pre-flight step failed; carries what ``run_cli`` should report/return.

    ``emit`` is the operator-facing error message; ``output`` is what the caller
    (and therefore the agent) receives as the command's output.
    """

    def __init__(self, code: int, emit: str, output: str | None = None) -> None:
        super().__init__(emit)
        self.code = code
        self.emit = emit
        self.output = emit if output is None else output


def final_status(name: str, exit_code: int, cancelled: bool) -> str:
    """Lifecycle status for a long-running command that just finished (pure).

    Order matters: a user cancel is not a failure (§5.4), and a non-zero
    ``/health`` means checks failed rather than the range breaking — that
    verdict lives per-host, so the range stays ``running``.
    """
    if cancelled:
        return "interrupted"
    if name == "/health":
        return "running"
    if exit_code:
        return "error"
    if name == "/destroy":
        return "destroyed"
    return "running"


async def _prepare_extra(
    session: dict[str, t.Any],
    session_id: str,
    name: str,
    extra: list[str],
) -> list[str]:
    """Resolve command arguments that need work before the CLI runs.

    Only ``/score`` does today: its report lives on the attack box and
    ``score --report`` takes a local path, so it's fetched into the session dir
    first (§5.2). Raises ``_Aborted`` if the fetch fails.
    """
    if name != "/score" or not extra:
        return extra
    try:
        rc_fetch, local, msg = await fetch.fetch_report(
            session, extra[0], lambda argv, cwd: _capture_for_turn(session_id, argv, cwd)
        )
    except ValueError as exc:
        raise _Aborted(1, str(exc)) from exc
    if rc_fetch != 0:
        raise _Aborted(rc_fetch, f"report fetch failed: {msg[-300:]}", msg)
    return [local, *extra[1:]]


async def _stream_output(app: t.Any, session_id: str, name: str, rc: t.Any) -> None:
    """Relay the process's live tail as command_progress, filtered per command."""
    async for line in rc.stream_lines():
        line = summary.strip_ansi(line)
        if name == "/instances":
            # A JSON blob — suppressed here, rendered as a formatted
            # instances_report table once the command finishes (§6.4).
            continue
        if name == "/health":
            # health-check --json streams NDJSON: show each check live as a
            # readable line; the final report line is parsed for the table.
            progress = _health_progress(line)
            if progress is None:
                continue
            line = progress
        await emit_event(
            app, session_id, "command_progress", {"line": line}, persist=False
        )


async def _emit_overlays(
    app: t.Any, session_id: str, name: str, output: str, exit_code: int
) -> None:
    """Apply and announce the command-specific range overlays (§6.4 / §6.3)."""
    if name == "/health":
        report = await hook.apply_health(app, session_id, output, exit_code)
        if report is not None:
            await emit_event(
                app,
                session_id,
                "health_report",
                {
                    "passed": report.get("passed", 0),
                    "failed": report.get("failed", 0),
                    "skipped": report.get("skipped", 0),
                    "checks": report.get("checks", []),
                },
            )
    elif name == "/instances":
        instances = parse_instances(output)
        if instances is not None:
            running = sum(
                1 for i in instances if str(i.get("state", "")).lower() == "running"
            )
            await emit_event(
                app,
                session_id,
                "instances_report",
                {
                    "instances": instances,
                    "total": len(instances),
                    "running": running,
                },
            )
    elif name == "/validate":
        report = summary.parse_validate_report(output)
        if report is not None:
            checks = report.get("checks") or []
            await emit_event(
                app,
                session_id,
                "validate_report",
                {
                    "passed": report.get("passed", 0),
                    "failed": report.get("failed", 0),
                    "warnings": report.get("warnings", 0),
                    "total": report.get("total_checks", len(checks)),
                    "categories": summary.validate_categories(checks),
                    # Only the failures travel per-check; 200+ passing rows would
                    # bloat every replay for no benefit.
                    "failures": [
                        {
                            "category": c.get("category", ""),
                            "name": c.get("name", ""),
                        }
                        for c in checks
                        if str(c.get("status", "")).upper() == "FAIL"
                    ],
                },
            )
    elif name == "/scrub":
        report = summary.parse_scrub_report(output)
        if report is not None:
            hosts = report.get("hosts") or []
            await emit_event(
                app,
                session_id,
                "scrub_report",
                {
                    "mode": report.get("mode", "unknown"),
                    "hosts": hosts,
                    "found": sum(int(h.get("found", 0)) for h in hosts),
                    "removed": sum(int(h.get("removed", 0)) for h in hosts),
                },
            )
    elif name == "/exec":
        results = summary.parse_json_array(output)
        if results is not None:
            # Host output is arbitrary and may be colourised (PowerShell 7 does
            # by default). The chat pane renders text, not a terminal.
            results = summary.clean_exec_results(results)
            await emit_event(
                app,
                session_id,
                "exec_report",
                {
                    "results": results,
                    "succeeded": sum(
                        1 for r in results if summary.exec_succeeded(r.get("status"))
                    ),
                    "total": len(results),
                },
            )
    elif name in ("/variant", "/extensions"):
        await hook.reseed(
            app,
            session_id,
            lambda argv, cwd: _capture_for_turn(session_id, argv, cwd),
        )


async def _capture_for_turn(
    session_id: str, argv: list[str], cwd: str
) -> tuple[int, str, str]:
    """Run a machine-readable helper as an owned subprocess of this turn."""
    runtime = _runtime(session_id)
    turn = runtime.turn
    if turn is not None:
        turn.commands_starting += 1
    try:
        command = await start_capture(argv, cwd)
    finally:
        if turn is not None:
            turn.commands_starting = max(0, turn.commands_starting - 1)

    if turn is not None and turn.cancelled:
        command.cancel()
    runtime.running.add(command)
    try:
        result = await command.communicate()
    finally:
        runtime.running.discard(command)

    turn = runtime.turn
    if command.cancelled or (turn is not None and turn.cancelled):
        raise asyncio.CancelledError
    return result


async def run_cli(
    app: t.Any, session_id: str, name: str, extra: list[str] | None = None
) -> tuple[int, str]:
    """Run a dreadgoad command through the full pipeline; return (exit_code, output).

    Emits command_run/command_progress/check_run + overlays, sets lifecycle
    status, registers the process on ``SessionRuntime`` for cancel. Shared by
    direct dispatch and the
    agent's ``run_dreadgoad`` tool, so both paths behave identically (§5.1).
    """
    session = await app.state.db.get_session(session_id)
    if session is None:
        await emit_event(app, session_id, "error", {"message": "session not found"})
        return 1, "session not found"

    try:
        extra = await _prepare_extra(session, session_id, name, list(extra or []))
    except _Aborted as exc:
        await emit_event(app, session_id, "error", {"message": exc.emit})
        return exc.code, exc.output

    try:
        argv = commands.build_argv(
            session, name, extra, repo_root=str(paths.repo_root())
        )
    except ValueError as exc:
        # Refused before anything ran — the args tried to retarget the range.
        # Surfaced as a normal failure so the agent reads the reason and can
        # retry without it, rather than the turn dying on an exception.
        await emit_event(app, session_id, "error", {"message": str(exc)})
        return 1, str(exc)

    await emit_event(
        app,
        session_id,
        "command_run",
        {"phase": "start", "command": name, "argv": argv},
    )

    # Name the command on the in-flight turn so a reconnecting client can still
    # warn before cancelling a destructive one.
    runtime = _runtime(session_id)
    turn = runtime.turn
    if turn is not None:
        turn.command = name

    cmd = commands.REGISTRY[name]
    if cmd.long_running:
        await app.state.sessions.set_status(session_id, "provisioning")

    if turn is not None:
        turn.commands_starting += 1
    try:
        rc = await start_command(argv, cwd=str(paths.repo_root()))
    except OSError as exc:
        # A missing/non-executable CLI is an ordinary command failure, not a
        # background-task crash. In particular, do not leave a long-running
        # command stuck at ``provisioning`` when no process ever started.
        message = f"failed to start {name}: {exc}"
        if cmd.long_running:
            await app.state.sessions.set_status(session_id, "error")
        await emit_event(app, session_id, "error", {"message": message})
        await emit_event(
            app,
            session_id,
            "command_run",
            {
                "phase": "end",
                "command": name,
                "exit_code": 1,
                "cancelled": False,
                "tail": message,
            },
        )
        return 1, message
    finally:
        if turn is not None:
            turn.commands_starting = max(0, turn.commands_starting - 1)
    # SIGINT→SIGKILL grace: give terraform/ansible a long runway to unwind on
    # SIGINT (killing terraform mid-apply strands a state lock); reads/local ops
    # can be hard-killed quickly if they ignore SIGINT.
    rc._KILL_GRACE = 300.0 if name in _SLOW_CANCEL else 12.0
    if turn is not None and turn.cancelled:
        rc.cancel()
    runtime.running.add(rc)
    try:
        await _stream_output(app, session_id, name, rc)
    finally:
        runtime.running.discard(rc)
    exit_code, output = rc.returncode, rc.output

    if cmd.long_running:
        await app.state.sessions.set_status(
            session_id, final_status(name, exit_code, rc.cancelled)
        )

    await emit_event(
        app,
        session_id,
        "command_run",
        {
            "phase": "end",
            "command": name,
            "exit_code": exit_code,
            # So the UI can say "cancelled" rather than the meaningless
            # "exit -2" an operator sees when they hit Esc.
            "cancelled": rc.cancelled,
            "tail": output[-2000:],
        },
    )

    # Cancellation belongs to the whole turn, not just this process. Do not run
    # post-command checks or hand a "failed" tool result back to the model: both
    # let an agent continue working and potentially launch the command again.
    turn = runtime.turn
    if rc.cancelled or (turn is not None and turn.cancelled):
        raise asyncio.CancelledError

    payload = await hook.run_check(
        app,
        session_id,
        lambda argv, cwd: _capture_for_turn(session_id, argv, cwd),
    )
    await emit_event(app, session_id, "check_run", payload)
    await _emit_overlays(app, session_id, name, output, exit_code)

    return exit_code, output


async def handle_message(app: t.Any, session_id: str, content: str) -> None:
    """Route a message: direct command → run_cli; agent command / free-text → agent."""
    await emit_event(app, session_id, "user_message", {"content": content})

    if commands.is_command(content):
        name, extra = commands.parse_command(content)
        session = await app.state.db.get_session(session_id)
        if session is None:
            await emit_event(app, session_id, "error", {"message": "session not found"})
            await emit_event(app, session_id, "agent_end", {"failed": True})
            return
        cmd = commands.REGISTRY[name]
        if cmd.dispatch == "direct":
            # Most direct commands are deterministic reads with a fixed verb;
            # extra operator tokens would land as bogus CLI args, so reject
            # cleanly instead of shelling out to a guaranteed CLI error (C4).
            # Gated on takes_args, not on dispatch: /scrub is direct *and*
            # accepts arguments (a dry-run token, CLI flags).
            if extra and not cmd.takes_args:
                await emit_event(
                    app,
                    session_id,
                    "error",
                    {"message": f"{name} takes no arguments (got: {' '.join(extra)})"},
                )
                await emit_event(app, session_id, "agent_end", {"failed": True})
                return
            exit_code, _ = await run_cli(app, session_id, name, extra)
            await emit_event(app, session_id, "agent_end", {"failed": exit_code != 0})
            return
        # dispatch="agent": expand to a structured prompt; the agent runs it via
        # its run_dreadgoad tool (robust arg interpretation, constrained).
        await _run_agent(app, session_id, commands.expand_command_prompt(name, extra))
        return

    await _run_agent(app, session_id, content)


async def _run_agent(app: t.Any, session_id: str, prompt: str) -> None:
    """Stream one agent turn (free-text or an expanded command) to the client."""
    agent = await _get_agent(app, session_id)
    if agent is None:
        await emit_event(app, session_id, "error", {"message": "session not found"})
        await emit_event(app, session_id, "agent_end", {"failed": True})
        return
    try:
        async with agent.stream(prompt) as events:
            async for event in events:
                formatted = format_event(event)
                if formatted:
                    kind = formatted.pop("kind")
                    await emit_event(app, session_id, kind, formatted)
    except Exception as exc:  # noqa: BLE001 - surface any agent error to the client
        await emit_event(app, session_id, "error", {"message": f"agent error: {exc}"})
        await emit_event(app, session_id, "agent_end", {"failed": True})


def _flatten(event: dict[str, t.Any]) -> dict[str, t.Any]:
    """Reshape a stored event to match what ``emit_event`` puts on the wire.

    The DB keeps the payload nested (``{seq, kind, ts, payload}``) but live
    events are flat (``{kind, **payload}``), and the client reads fields off the
    event directly. Replaying the stored shape renders blank — every field the
    UI wants sits one level down. Payload last, mirroring ``emit_event``.
    """
    payload = event.get("payload") or {}
    return {
        "seq": event.get("seq"),
        "ts": event.get("ts"),
        "kind": event["kind"],
        **payload,
    }


async def replay(app: t.Any, session_id: str) -> None:
    """Send chat-kind history for a session to its current socket on (re)connect."""
    events = await app.state.db.get_events(session_id, kinds=CHAT_KINDS)
    runtime = _runtimes.get(session_id)
    ws = runtime.conn if runtime is not None else None
    if ws is None:
        return
    # Resume is the reconnect handshake, so it also reports whether a turn is
    # still running server-side. Without this a reloaded browser shows an idle
    # pane while a deploy is mid-flight, with no working indicator and no way to
    # cancel — the failure mode that made a wedged /health hard to spot.
    turn = runtime.turn if runtime is not None else None
    await ws.send_text(
        json.dumps(
            {
                "session_id": session_id,
                "kind": "history",
                "events": [_flatten(e) for e in events],
                "active": turn is not None,
                "started_at": turn.started_at if turn else None,
                "command": turn.command if turn else None,
            }
        )
    )
