"""Tests for tool_use/tool_result pairing repair.

Built on real ``rigging.Message`` objects rather than stand-ins: the bug being
guarded against is that a message list is rejected by a provider, so a fake with
the right attribute names would prove nothing about the shape actually sent.
"""

from __future__ import annotations

import asyncio
import sys
import types
import typing as t
from contextlib import asynccontextmanager
from pathlib import Path

from rigging import Message
from rigging.tools.base import FunctionCall, ToolCall

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

from backend.thread_repair import (  # noqa: E402
    MISSING_RESULT_NOTE,
    repair_tool_pairing,
)


def _call(call_id: str, name: str = "run_dreadgoad") -> ToolCall:
    return ToolCall(
        id=call_id,
        type="function",
        function=FunctionCall(name=name, arguments='{"command": "/health"}'),
    )


def _assistant(*call_ids: str, content: str = "") -> Message:
    return Message(
        role="assistant", content=content, tool_calls=[_call(c) for c in call_ids]
    )


def _result(call_id: str, content: str = "ok") -> Message:
    return Message(role="tool", tool_call_id=call_id, content=content)


def _pairs(messages: list[Message]) -> list[tuple[str, str | None]]:
    """The (role, tool id) spine of a thread — what the provider validates."""
    return [(m.role, m.tool_call_id or None) for m in messages]


def _assert_well_formed(messages: list[Message]) -> None:
    """Assert the invariant the providers enforce, independently of the repair.

    Deliberately re-derived here rather than reusing the module's own helpers:
    a test that checks a function against itself cannot fail.
    """
    index = 0
    while index < len(messages):
        message = messages[index]
        assert message.role != "tool", f"result answering nothing at {index}"
        expected = [c.id for c in (message.tool_calls or [])]
        index += 1
        answered: list[str] = []
        while index < len(messages) and messages[index].role == "tool":
            call_id = messages[index].tool_call_id
            # A tool message carrying no id answers nothing, which is the very
            # thing this helper exists to catch. Without the assert it would
            # compare as None against a real id and surface as a confusing
            # mismatch rather than the malformed message it is.
            assert call_id is not None, f"tool result at {index} has no tool_call_id"
            answered.append(call_id)
            index += 1
        assert answered == expected, (
            f"at message {index}: calls {expected} answered by {answered}"
        )


def test_orphaned_call_gets_a_result() -> None:
    # The exact shape of the incident: a tool call whose result never arrived
    # because the operator cancelled the command mid-run.
    messages = [
        Message(role="user", content="run /health"),
        _assistant("toolu_01U71"),
    ]
    repaired = repair_tool_pairing(messages)

    assert repaired == ["toolu_01U71"]
    assert _pairs(messages) == [
        ("user", None),
        ("assistant", None),
        ("tool", "toolu_01U71"),
    ]
    assert MISSING_RESULT_NOTE in messages[-1].content
    _assert_well_formed(messages)
    print("PASS test_orphaned_call_gets_a_result")


def test_well_formed_thread_is_untouched() -> None:
    # The sweep runs before every turn, so the overwhelmingly common case is a
    # thread that needs nothing. It must not be rewritten at all.
    messages = [
        Message(role="user", content="hi"),
        _assistant("a"),
        _result("a"),
        Message(role="assistant", content="done"),
    ]
    before = list(messages)
    snapshot = _pairs(messages)

    assert repair_tool_pairing(messages) == []
    assert _pairs(messages) == snapshot
    # Identity, not equality: the list must not have been rebuilt.
    assert all(x is y for x, y in zip(messages, before, strict=True))
    print("PASS test_well_formed_thread_is_untouched")


def test_partial_answers_across_parallel_calls() -> None:
    # Two calls in one assistant message, one answered and one cancelled. Only
    # the missing one is synthesised, and the real result is preserved verbatim.
    messages = [
        _assistant("a", "b"),
        _result("a", content="instances: 7 running"),
    ]
    assert repair_tool_pairing(messages) == ["b"]

    assert _pairs(messages) == [("assistant", None), ("tool", "a"), ("tool", "b")]
    assert messages[1].content == "instances: 7 running"
    assert MISSING_RESULT_NOTE in messages[2].content
    _assert_well_formed(messages)
    print("PASS test_partial_answers_across_parallel_calls")


def test_results_follow_call_order() -> None:
    # Answers arriving out of order are re-emitted in the order the calls were
    # made, so the two sequences read the same way in a transcript.
    messages = [_assistant("a", "b", "c"), _result("c"), _result("a")]
    assert sorted(repair_tool_pairing(messages)) == ["b"]

    assert _pairs(messages) == [
        ("assistant", None),
        ("tool", "a"),
        ("tool", "b"),
        ("tool", "c"),
    ]
    _assert_well_formed(messages)
    print("PASS test_results_follow_call_order")


def test_stray_and_duplicate_results_are_dropped() -> None:
    # A result answering nothing is as fatal as a missing one. Both a leading
    # stray and a duplicate answer must go.
    messages = [
        _result("ghost"),
        _assistant("a"),
        _result("a"),
        _result("a", content="duplicate"),
    ]
    repaired = repair_tool_pairing(messages)

    assert sorted(repaired) == ["a", "ghost"]
    assert _pairs(messages) == [("assistant", None), ("tool", "a")]
    assert messages[1].content == "ok"
    _assert_well_formed(messages)
    print("PASS test_stray_and_duplicate_results_are_dropped")


def test_multiple_orphans_across_a_long_thread() -> None:
    # Several interrupted turns stacked up. Every one is repaired in one pass,
    # and the result is well-formed end to end.
    messages = [
        Message(role="user", content="one"),
        _assistant("a"),
        Message(role="user", content="two"),
        _assistant("b"),
        _result("b"),
        Message(role="user", content="three"),
        _assistant("c", "d"),
    ]
    assert sorted(repair_tool_pairing(messages)) == ["a", "c", "d"]
    _assert_well_formed(messages)
    assert len(messages) == 10
    print("PASS test_multiple_orphans_across_a_long_thread")


def test_repair_is_idempotent() -> None:
    # The sweep runs before every turn, so a repaired thread is swept again.
    messages = [_assistant("a")]
    assert repair_tool_pairing(messages) == ["a"]
    snapshot = _pairs(messages)

    assert repair_tool_pairing(messages) == []
    assert _pairs(messages) == snapshot
    print("PASS test_repair_is_idempotent")


def test_empty_thread() -> None:
    messages: list[Message] = []
    assert repair_tool_pairing(messages) == []
    assert messages == []
    print("PASS test_empty_thread")


def test_repaired_thread_serialises_for_the_api() -> None:
    """The synthetic message must survive the same serialisation as a real one.

    A repair that produced an object rigging cannot render would trade a 400 for
    a crash, so this asserts on the serialised form rather than the model.
    """
    messages = [_assistant("a"), _result("b")]  # one orphan, one stray
    repair_tool_pairing(messages)

    payloads = [m.to_openai(compatibility_flags={"content_as_str"}) for m in messages]
    assert payloads[1]["role"] == "tool"
    assert payloads[1]["tool_call_id"] == "a"
    assert isinstance(payloads[1]["content"], str)
    assert payloads[1]["content"].strip()
    print("PASS test_repaired_thread_serialises_for_the_api")


async def test_cancelled_command_yields_a_paired_result_and_stops() -> None:
    """The root fix, asserted where the 400 was actually created.

    Driven through ``handle_tool_call`` — rigging's own dispatch — because that
    is what decides whether a tool_result message exists. Calling the undecorated
    function would skip the exact layer whose ``except Exception`` misses
    CancelledError, and so would prove nothing about the bug.
    """
    from backend import agent as agent_mod

    async def cancelling_run_cli(
        app: object, session_id: str, command: str, args: list[str]
    ) -> tuple[int, str]:
        # Exactly what command_runner.py does when the operator cancels.
        raise asyncio.CancelledError

    tool_obj = agent_mod._make_run_dreadgoad(object(), "s-1", cancelling_run_cli)
    message, stop = await tool_obj.handle_tool_call(_call("toolu_01U71"))

    # The pairing the provider checks for, which is what was missing.
    assert message.role == "tool"
    assert message.tool_call_id == "toolu_01U71"
    # stop=True is what makes the agent raise Finish instead of generating
    # again, so the cancel cannot turn into a retry.
    assert stop is True
    _assert_well_formed([_assistant("toolu_01U71"), message])
    print("PASS test_cancelled_command_yields_a_paired_result_and_stops")


async def test_genuine_task_cancellation_is_not_swallowed() -> None:
    """A real task.cancel() must still tear the turn down.

    The conversion keys on ``cancelling()``, so this is the case proving it
    discriminates rather than swallowing every CancelledError — which would
    trade a wedged session for a turn that ignores shutdown.
    """
    from backend import agent as agent_mod

    started = asyncio.Event()

    async def hanging_run_cli(
        app: object, session_id: str, command: str, args: list[str]
    ) -> tuple[int, str]:
        started.set()
        await asyncio.sleep(3600)
        return 0, ""

    tool_obj = agent_mod._make_run_dreadgoad(object(), "s-1", hanging_run_cli)

    task = asyncio.create_task(tool_obj.handle_tool_call(_call("toolu_x")))
    await started.wait()
    task.cancel()

    try:
        await task
    except asyncio.CancelledError:
        pass
    else:
        raise AssertionError("genuine cancellation was swallowed")

    assert task.cancelled()
    print("PASS test_genuine_task_cancellation_is_not_swallowed")


async def test_cancellederror_is_silently_dropped_by_the_agent_plumbing() -> None:
    """Pin the library behaviour the fix exists because of.

    The diagnosis rests on one non-obvious claim: a CancelledError raised inside
    a tool call does not reach the agent as an error — it is swallowed, and the
    join loop ends *normally* with the tool having produced nothing. That is why
    the run continued and hit a 400 rather than stopping.

    Asserted against the real ``join_generators`` so the fix does not rest on my
    reading of it, and so a library version that starts propagating cancellation
    fails here loudly instead of silently changing what the fix is for.
    """
    from dreadnode.util import join_generators

    async def cancelled_tool_call() -> t.AsyncGenerator[str, None]:
        yield "tool_start"
        raise asyncio.CancelledError  # what run_cli raises on an operator cancel

    async def healthy_tool_call() -> t.AsyncGenerator[str, None]:
        yield "tool_start"
        yield "tool_end"

    seen: list[str] = []
    async for event in join_generators(cancelled_tool_call(), healthy_tool_call()):
        seen.append(event)

    # No exception escaped, so the agent loop simply carried on...
    assert seen.count("tool_start") == 2
    # ...with only one of the two calls having produced a result. That absent
    # "tool_end" is the unpaired tool_use the provider rejected.
    assert seen.count("tool_end") == 1
    print("PASS test_cancellederror_is_silently_dropped_by_the_agent_plumbing")


async def test_run_agent_heals_a_poisoned_thread_before_streaming() -> None:
    """The sweep in _run_agent must repair a cached agent's thread in place.

    This is the recovery path for a session that is *already* wedged, which is
    not hypothetical: a generation error is caught into ``error`` and breaks the
    loop (agent/agent.py:603), and since the console streams with
    ``commit="always"`` the unpaired list is then written back to the thread. A
    session that hits this cannot talk again until something repairs it.

    Asserted through ``_run_agent`` rather than the pure function so the wiring
    is covered too — the repair must happen *before* stream() copies the thread.
    """
    from backend import chat, chat_runtime

    class PoisonedAgent:
        """Cached agent whose thread carries an orphan from a killed turn."""

        def __init__(self) -> None:
            self.thread = types.SimpleNamespace(
                messages=[
                    Message(role="user", content="run /health"),
                    _assistant("toolu_01U71"),
                ]
            )
            self.seen: list[t.Any] | None = None

        def stream(self, prompt: str):  # noqa: ANN202
            # Snapshot what stream() would deepcopy — the repair must already
            # have happened by now, or the provider sees the unpaired list.
            self.seen = list(self.thread.messages)
            return self._cm()

        @asynccontextmanager
        async def _cm(self):  # noqa: ANN202
            async def _events():
                return
                yield  # pragma: no cover

            yield _events()

    agent = PoisonedAgent()
    session_id = "s-poisoned"
    chat_runtime.runtime(session_id).agent = agent

    events: list[tuple[str, dict[str, t.Any]]] = []

    async def fake_append_event(sid: str, kind: str, payload: dict) -> None:
        events.append((kind, payload))

    app = types.SimpleNamespace(
        state=types.SimpleNamespace(
            db=types.SimpleNamespace(append_event=fake_append_event),
            sessions=None,
        )
    )

    try:
        await chat._run_agent(app, session_id, "what happened?")
    finally:
        chat_runtime.runtimes.pop(session_id, None)

    assert agent.seen is not None, "stream() was never reached"
    _assert_well_formed(agent.seen)
    assert [m.role for m in agent.seen] == ["user", "assistant", "tool"]
    # The operator is told, rather than the recovery happening silently.
    assert any(
        k == "status" and "Recovered" in p.get("content", "") for k, p in events
    ), events
    print("PASS test_run_agent_heals_a_poisoned_thread_before_streaming")


def main() -> None:
    test_orphaned_call_gets_a_result()
    test_well_formed_thread_is_untouched()
    test_partial_answers_across_parallel_calls()
    test_results_follow_call_order()
    test_stray_and_duplicate_results_are_dropped()
    test_multiple_orphans_across_a_long_thread()
    test_repair_is_idempotent()
    test_empty_thread()
    test_repaired_thread_serialises_for_the_api()
    asyncio.run(test_cancellederror_is_silently_dropped_by_the_agent_plumbing())
    asyncio.run(test_cancelled_command_yields_a_paired_result_and_stops())
    asyncio.run(test_genuine_task_cancellation_is_not_swallowed())
    asyncio.run(test_run_agent_heals_a_poisoned_thread_before_streaming())
    print("ALL PASS")


if __name__ == "__main__":
    main()
