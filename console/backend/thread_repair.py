"""Keep a conversation thread's tool_use/tool_result blocks paired.

Every provider enforces the same rule: an assistant message carrying `tool_use`
blocks must be followed immediately by a `tool_result` for each of those ids.
Break it and the next request is rejected outright —

    messages.9: `tool_use` ids were found without `tool_result` blocks
    immediately after: toolu_01U71NpNTFvSv7fqdye9PW6L

— with a 400 from Anthropic, Bedrock, Vertex and Azure alike, because the fault
is in the message list rather than any one provider.

An unpaired list is unrecoverable on its own: the offending messages are already
in the thread, so *every* later turn re-sends them and fails the same way. The
session is wedged until the process restarts. That asymmetry is the reason this
runs as a sweep before each turn rather than only where a known bug can orphan a
call — the cost of a needless check is a list walk, and the cost of a missed one
is a session that can never speak again.

The specific bug that motivated it is fixed at its source (see run_dreadgoad in
agent.py). This is the backstop for the next one.
"""

from __future__ import annotations

import typing as t

from rigging import Message

# What a synthesised result says. Phrased for the model, which reads this as the
# outcome of a call it made: state plainly that the result is unknown, so it
# neither invents an outcome nor assumes failure.
MISSING_RESULT_NOTE = (
    "This tool call produced no result — the run was interrupted before it "
    "returned. Its outcome is unknown; do not assume it succeeded or failed. "
    "Re-run it if you still need the answer."
)


def _tool_call_ids(message: t.Any) -> list[str]:
    """Ids of the tool calls a message makes, in the order it made them."""
    calls = getattr(message, "tool_calls", None) or []
    return [call.id for call in calls if getattr(call, "id", None)]


def repair_tool_pairing(
    messages: list[t.Any], *, note: str = MISSING_RESULT_NOTE
) -> list[str]:
    """Pair every tool call in ``messages`` with a result, editing in place.

    Two faults are repaired, both of which are a hard 400:

    * a tool call with no result — a synthetic result is inserted for it
    * a result answering nothing — dropped, since no call declared its id

    Results are re-emitted in the order their calls were made. Providers do not
    require that, but a list where the two sequences agree is easier to read in
    a transcript, and reordering an already-valid block changes nothing about
    how it is interpreted.

    Returns the ids that were repaired (synthesised or dropped), so a caller can
    log that it happened. A well-formed thread returns an empty list and is left
    byte-for-byte alone — this is safe to run before every turn.
    """
    repaired: list[str] = []
    out: list[t.Any] = []
    index = 0
    total = len(messages)

    while index < total:
        message = messages[index]

        # A result at this position answers nothing: either it leads the thread
        # or the message before it made no calls. Providers reject it exactly
        # as they reject an unanswered call.
        if getattr(message, "role", None) == "tool":
            repaired.append(getattr(message, "tool_call_id", None) or "<no id>")
            index += 1
            continue

        out.append(message)
        expected = _tool_call_ids(message)
        index += 1
        if not expected:
            continue

        # Consume the run of results that follows, keeping the first answer to
        # each declared id. A duplicate answer is as invalid as a missing one.
        answers: dict[str, t.Any] = {}
        while index < total and getattr(messages[index], "role", None) == "tool":
            result = messages[index]
            call_id = getattr(result, "tool_call_id", None)
            if call_id in expected and call_id not in answers:
                answers[call_id] = result
            else:
                repaired.append(call_id or "<no id>")
            index += 1

        for call_id in expected:
            if call_id not in answers:
                answers[call_id] = Message(
                    role="tool", tool_call_id=call_id, content=note
                )
                repaired.append(call_id)

        out.extend(answers[call_id] for call_id in expected)

    if repaired:
        messages[:] = out
    return repaired
