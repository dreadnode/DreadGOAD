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
import typing as t
from copy import deepcopy

from rigging import Message

from . import chat_events, chat_runtime, command_runner, commands, paths, thread_repair
from .agent import create_agent

# Public facade used by server.py. Internal state remains owned and tested in
# chat_runtime rather than being mirrored here.
active_turn = chat_runtime.active_turn
begin_cleanup = chat_runtime.begin_cleanup
release_cleanup = chat_runtime.release_cleanup
session_closing = chat_runtime.session_closing
register_conn = chat_runtime.register_conn
unregister_conn = chat_runtime.unregister_conn
cancel_session = chat_runtime.cancel_session
cleanup_session = chat_runtime.cleanup_session
cleanup_all = chat_runtime.cleanup_all
emit_event = chat_events.emit_event
replay = chat_events.replay
run_cli = command_runner.run_cli


TURN_BUSY_MESSAGE = (
    "A turn is already running for this session; wait or cancel it first."
)


async def _save_thread(app: t.Any, session_id: str, agent: t.Any) -> None:
    """Persist the agent's conversation thread to the meta table."""
    thread = getattr(agent, "thread", None)
    if thread is None:
        return
    serialized = [msg.model_dump(mode="json") for msg in thread.messages]
    await app.state.db.set_meta(f"thread:{session_id}", serialized)


async def _load_thread(app: t.Any, session_id: str) -> list[Message] | None:
    """Load a persisted thread, returning deserialized Messages or None."""
    raw = await app.state.db.get_meta(f"thread:{session_id}")
    if raw is None:
        return None
    return [Message.model_validate(m) for m in raw]


def dispatch(app: t.Any, session_id: str, content: str) -> asyncio.Task[t.Any] | None:
    """Start one background turn, or reject it if the session is busy (§6.4, §7).

    The WS recv loop calls this and immediately keeps reading, so `cancel` and
    other sessions' messages are handled while a long op streams (§5.4).
    Admission is reserved synchronously, before the task can yield, so two rapid
    messages cannot both slip past the check and queue behind the session lock.
    Tasks are kept in ``_tasks`` so they survive the connection closing; emits
    target the session's *current* socket (`SessionRuntime.conn`).
    """

    runtime = chat_runtime.runtime(session_id)
    if runtime.turn is not None or runtime.closing:
        return None

    turn = chat_runtime.TurnState()
    runtime.turn = turn

    async def _runner() -> None:
        try:
            turn.started = True
            if turn.cancelled:
                raise asyncio.CancelledError
            async with runtime.lock:
                await handle_message(app, session_id, content)
        except asyncio.CancelledError:
            await emit_event(
                app,
                session_id,
                "agent_end",
                {"failed": False, "cancelled": True},
            )
            raise
        except Exception:
            # Any non-cancel exception (db error, corrupt thread, agent setup
            # failure) must still release the frontend's processing state.
            try:
                await emit_event(app, session_id, "agent_end", {"failed": True})
            except Exception:  # noqa: BLE001
                pass
            raise
        finally:
            if runtime.turn is turn:
                runtime.turn = None

    try:
        task = asyncio.create_task(_runner())
    except Exception:
        if runtime.turn is turn:
            runtime.turn = None
        raise
    turn.task = task
    chat_runtime.tasks.add(task)
    task.add_done_callback(lambda finished: _turn_task_done(session_id, turn, finished))
    return task


def _turn_task_done(
    session_id: str, turn: chat_runtime.TurnState, task: asyncio.Task[t.Any]
) -> None:
    """Release a finished turn and observe any exception it carried.

    ``dispatch`` deliberately runs turns in the background, so most tasks are
    never awaited by a caller. Retrieving the exception here prevents an
    unexpected pipeline failure from becoming an unobserved "Task exception was
    never retrieved" warning; focused tests may still await the task and receive
    the same exception normally.
    """
    chat_runtime.tasks.discard(task)
    # A task cancelled before its coroutine first runs never enters the
    # coroutine's ``finally`` block, so release its admission here as well.
    runtime = chat_runtime.runtimes.get(session_id)
    if runtime is not None and runtime.turn is turn:
        runtime.turn = None
    if runtime is not None:
        chat_runtime.discard_if_idle(session_id, runtime)
    if not task.cancelled():
        task.exception()


async def _get_agent(app: t.Any, session_id: str) -> t.Any | None:
    runtime = chat_runtime.runtime(session_id)
    if runtime.agent is not None:
        return runtime.agent
    session = await app.state.db.get_session(session_id)
    if session is None:
        return None
    agent = create_agent(
        # Falls back to the shared default rather than a literal of its own:
        # a session row written before the model column existed, or with a
        # blank value, should still run on whatever the console is configured
        # for -- not on a string frozen into this module.
        session.get("model") or paths.default_model(),
        session,
        app,
        session_id,
        run_cli,
    )
    messages = await _load_thread(app, session_id)
    if messages is not None:
        agent.thread.messages = messages
        thread_repair.repair_tool_pairing(agent.thread.messages)
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
    async with chat_runtime.session_lock(session_id):
        session = await app.state.db.get_session(session_id)
        if session is None:
            return None
        session["model"] = new_model
        await app.state.db.upsert_session(session)

        runtime = chat_runtime.runtime(session_id)
        old = runtime.agent
        if old is not None:
            history = deepcopy(old.thread.messages)
            fresh = create_agent(new_model, session, app, session_id, run_cli)
            fresh.thread.messages = history
            runtime.agent = fresh
            await _save_thread(app, session_id, fresh)
        # else: no live agent — _get_agent will build with the new model next turn.

    await emit_event(
        app, session_id, "status", {"content": f"Model changed to {new_model}."}
    )
    return session


async def _inject_direct_note(
    app: t.Any, session_id: str, name: str, exit_code: int
) -> None:
    """Add a note to the agent thread so the LLM knows a direct command ran.

    Direct commands bypass the agent entirely, so without this the LLM has
    no record that /destroy, /instances, etc. happened between its turns.
    Best-effort: agent setup failures must not break direct commands.
    """
    try:
        agent = await _get_agent(app, session_id)
    except Exception:  # noqa: BLE001
        return
    if agent is None:
        return
    thread = getattr(agent, "thread", None)
    if thread is None:
        return

    status = "succeeded" if exit_code == 0 else f"failed (exit {exit_code})"
    thread.messages.extend([
        Message(role="user", content=f"[System: the operator ran {name} directly. It {status}.]"),
        Message(role="assistant", content=f"Noted — {name} {status}."),
    ])
    await _save_thread(app, session_id, agent)


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
            await _inject_direct_note(app, session_id, name, exit_code)
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
    # An unpaired tool call in the thread is rejected by the provider before the
    # turn starts, and it is re-sent by every turn after this one, so a single
    # orphan silently ends the session's ability to talk. Sweeping here costs a
    # list walk and leaves a well-formed thread untouched.
    thread = getattr(agent, "thread", None)
    if thread is not None:
        repaired = thread_repair.repair_tool_pairing(thread.messages)
        if repaired:
            await emit_event(
                app,
                session_id,
                "status",
                {
                    "content": (
                        f"Recovered {len(repaired)} unfinished tool call(s) from "
                        "an interrupted turn."
                    )
                },
            )

    try:
        async with agent.stream(prompt) as events:
            async for event in events:
                formatted = chat_events.format_agent_event(event)
                if formatted:
                    kind = formatted.pop("kind")
                    await emit_event(app, session_id, kind, formatted)
    except Exception as exc:  # noqa: BLE001 - surface any agent error to the client
        await emit_event(app, session_id, "error", {"message": f"agent error: {exc}"})
        await emit_event(app, session_id, "agent_end", {"failed": True})
    finally:
        await _save_thread(app, session_id, agent)
