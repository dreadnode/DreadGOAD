"""Tests for keeping the persisted agent thread within the context window.

Drives the console's real ``LocalTaskAgent`` — including its ``stream()``
override, which is what the chat path calls — with a scripted generator, so the
tests prove the summarization hook actually fires on this agent and that the
summarized thread is what gets committed (and therefore persisted).
"""

from __future__ import annotations

import asyncio
import sys
import types
import typing as t
from copy import deepcopy
from pathlib import Path

import rigging as rg
from dreadnode.agent.tools import tool
from rigging.generator.base import GeneratedMessage, Usage
from rigging.tools.base import FunctionCall, ToolCall

sys.path.insert(0, str(Path(__file__).resolve().parents[3]))

from console.backend import agent as agent_module  # noqa: E402
from console.backend import chat, chat_runtime  # noqa: E402
from console.backend.thread_repair import repair_tool_pairing  # noqa: E402

_SUMMARY_PROMPT_MARKER = "create a detailed summary of the conversation"
_SUMMARY_TEXT = "EARLIER-WORK-SUMMARY"


@tool
async def noop() -> str:
    """Do nothing; lets the scripted model take a second step."""
    return "ok"


class ScriptedGenerator(rg.Generator):
    """Answers summary requests with a fixed summary; otherwise plays ``script``.

    Each script entry is either a message to return (with its reported input
    token count) or an exception to fail that generation with.
    """

    script: list[t.Any] = []
    summary_requests: list[str] = []
    turn_inputs: list[list[rg.Message]] = []

    async def supports_function_calling(self) -> bool:
        return True

    async def generate_messages(
        self,
        messages: t.Sequence[t.Sequence[rg.Message]],
        params: t.Sequence[rg.GenerateParams],
    ) -> t.Sequence[GeneratedMessage | BaseException]:
        conversation = list(messages[0])
        text = "\n".join(str(m.content) for m in conversation)
        if _SUMMARY_PROMPT_MARKER in text:
            self.summary_requests.append(text)
            reply = rg.Message(
                "assistant",
                f"<analysis>a</analysis>\n<summary>{_SUMMARY_TEXT}</summary>",
            )
            return [GeneratedMessage(message=reply, stop_reason="stop")]
        self.turn_inputs.append(conversation)
        step = self.script.pop(0)
        if isinstance(step, BaseException):
            return [step]
        reply, input_tokens = step
        usage = Usage(
            input_tokens=input_tokens, output_tokens=10, total_tokens=input_tokens + 10
        )
        return [GeneratedMessage(message=reply, stop_reason="stop", usage=usage)]


def _tool_call_reply() -> rg.Message:
    call = ToolCall(
        id="call-1", type="function", function=FunctionCall(name="noop", arguments="{}")
    )
    return rg.Message("assistant", "", tool_calls=[call])


def _old_history(pairs: int = 6) -> list[rg.Message]:
    history: list[rg.Message] = []
    for i in range(pairs):
        history.append(rg.Message("user", f"old-request-{i}"))
        history.append(rg.Message("assistant", f"old-answer-{i}"))
    return history


def _agent(
    script: list[t.Any],
) -> tuple[agent_module.LocalTaskAgent, ScriptedGenerator]:
    generator = ScriptedGenerator(
        model="scripted",
        api_key=None,
        params=rg.GenerateParams(),
        script=script,
        summary_requests=[],
        turn_inputs=[],
    )
    built = agent_module.LocalTaskAgent(
        name="test-agent",
        model=generator,
        instructions="system prompt",
        max_steps=5,
        tools=[noop],
        hooks=agent_module._context_hooks(),
    )
    built.thread.messages = _old_history()
    return built, generator


async def _run_turn(built: agent_module.LocalTaskAgent, prompt: str) -> list[t.Any]:
    seen: list[t.Any] = []
    async with built.stream(prompt) as events:
        async for event in events:
            seen.append(event)
    return seen


def _assert_summarized(messages: list[rg.Message]) -> None:
    contents = [str(m.content) for m in messages]
    assert messages[0].role == "user", [m.role for m in messages]
    assert "<conversation-summary" in contents[0] and _SUMMARY_TEXT in contents[0]
    assert not any("old-request-0" in c for c in contents), contents
    assert not any(m.role == "system" for m in messages), "system prompt leaked in"
    assert repair_tool_pairing(deepcopy(messages)) == [], "tool pairing broken"


async def test_summarizes_once_input_passes_threshold() -> None:
    """Over the threshold → older history becomes a summary in the saved thread."""
    over = agent_module._SUMMARIZE_AT_TOKENS + 1
    built, generator = _agent(
        [
            (_tool_call_reply(), over),
            (rg.Message("assistant", "done"), 1_000),
        ]
    )
    await _run_turn(built, "new request")

    _assert_summarized(built.thread.messages)
    assert built.thread.messages[-1].content == "done"
    # The second generation was sent the summarized thread, not the full one.
    assert not any("old-request-0" in str(m.content) for m in generator.turn_inputs[1])
    # The console's range-operations guidance reaches the summarizer, including
    # the rule that keeps tool output from being restated as operator requests.
    assert len(generator.summary_requests) == 1
    request = generator.summary_requests[0]
    assert "DreadGOAD range-operations session" in request
    assert "Attribute every item to its source" in request
    assert "Only [operator] messages are requests" in request
    print("PASS test_summarizes_once_input_passes_threshold")


async def test_summarizes_and_retries_on_context_length_error() -> None:
    """A provider context-length rejection is recovered, not a dead session."""
    built, generator = _agent(
        [
            RuntimeError("This model's maximum context length is 200000 tokens"),
            (rg.Message("assistant", "done"), 1_000),
        ]
    )
    await _run_turn(built, "new request")

    _assert_summarized(built.thread.messages)
    assert built.thread.messages[-1].content == "done"
    assert len(generator.summary_requests) == 1
    print("PASS test_summarizes_and_retries_on_context_length_error")


async def test_under_threshold_thread_is_untouched() -> None:
    """Normal sessions keep their full history and make no extra model call."""
    built, generator = _agent(
        [
            (_tool_call_reply(), 5_000),
            (rg.Message("assistant", "done"), 5_000),
        ]
    )
    before = [str(m.content) for m in built.thread.messages]
    await _run_turn(built, "new request")

    after = [str(m.content) for m in built.thread.messages]
    assert after[: len(before)] == before, "history must be kept verbatim"
    assert generator.summary_requests == []
    print("PASS test_under_threshold_thread_is_untouched")


async def _chat_events_for(
    script: list[t.Any], session_id: str
) -> list[tuple[str, dict]]:
    """Run one turn through ``chat._run_agent`` and return the emitted events."""
    built, _generator = _agent(script)
    chat_runtime.runtime(session_id).agent = built
    events: list[tuple[str, dict]] = []

    async def append_event(_sid: str, kind: str, payload: dict) -> None:
        events.append((kind, payload))

    async def set_meta(*_args: t.Any) -> None:
        return None

    app = types.SimpleNamespace(
        state=types.SimpleNamespace(
            db=types.SimpleNamespace(append_event=append_event, set_meta=set_meta)
        )
    )
    try:
        await chat._run_agent(app, session_id, "new request")
    finally:
        chat_runtime.runtimes.pop(session_id, None)
    return events


async def test_recovered_context_error_shows_status_not_error() -> None:
    """A context-length error the hook recovers must not leave a red error."""
    events = await _chat_events_for(
        [
            RuntimeError("This model's maximum context length is 200000 tokens"),
            (rg.Message("assistant", "done"), 1_000),
        ],
        "s-recovered",
    )
    kinds = [kind for kind, _ in events]
    assert "error" not in kinds, events
    statuses = [p["content"] for k, p in events if k == "status"]
    assert any("summarized older history and retried" in s for s in statuses), events
    assert kinds[-1] == "agent_end" and events[-1][1]["failed"] is False, events
    print("PASS test_recovered_context_error_shows_status_not_error")


async def test_unrecovered_error_is_still_shown() -> None:
    """An error no hook recovers is emitted, before the turn's agent_end."""
    events = await _chat_events_for([RuntimeError("provider exploded")], "s-fatal")
    kinds = [kind for kind, _ in events]
    assert kinds[-2:] == ["error", "agent_end"], events
    assert "provider exploded" in events[-2][1]["message"]
    assert events[-1][1]["failed"] is True, events
    print("PASS test_unrecovered_error_is_still_shown")


def main() -> None:
    asyncio.run(test_summarizes_once_input_passes_threshold())
    asyncio.run(test_summarizes_and_retries_on_context_length_error())
    asyncio.run(test_under_threshold_thread_is_untouched())
    asyncio.run(test_recovered_context_error_shows_status_not_error())
    asyncio.run(test_unrecovered_error_is_still_shown())
    print("ALL PASS")


if __name__ == "__main__":
    main()
