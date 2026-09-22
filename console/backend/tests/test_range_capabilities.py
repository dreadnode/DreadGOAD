"""Focused tests for the CLI-backed per-session command catalog."""

from __future__ import annotations

import asyncio
import json
import pathlib
import sys
import tempfile
from types import SimpleNamespace

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import (  # noqa: E402
    chat_events,
    chat_runtime,
    command_runner,
    commands,
    hook,
    range_capabilities,
)


def test_load_validates_and_keys_capabilities() -> None:
    async def run() -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "ansible").mkdir()
            config = root / "dreadgoad.yaml"
            config.write_text("environments:\n  dev: {}\n")
            session = {
                "anchor": {"config_path": str(config), "env": "dev"},
            }

            async def fake_capture(argv: list[str], cwd: str) -> tuple[int, str, str]:
                assert argv[-2:] == ["range", "capabilities"]
                assert pathlib.Path(cwd).resolve() == root.resolve()
                return (
                    0,
                    json.dumps(
                        {
                            "range": "service",
                            "agent_prompt": "This range models a web service.",
                            "commands": [
                                {
                                    "name": "validate",
                                    "supported": True,
                                    "description": "Validate the service",
                                    "protocol": "validate/v1",
                                    "handler_type": "executable",
                                },
                                {"name": "score", "supported": False},
                                {"name": "health", "supported": False},
                                {"name": "reset", "supported": False},
                                {"name": "scrub", "supported": False},
                            ],
                        }
                    ),
                    "",
                )

            found = await range_capabilities.load(
                session, str(root), capture_command=fake_capture
            )
            assert found["/validate"].get("description") == "Validate the service"
            assert found["/score"]["supported"] is False
            assert found.agent_prompt == "This range models a web service."

    asyncio.run(run())


def test_load_rejects_unknown_or_duplicate_capabilities() -> None:
    async def run(payload: dict[str, object]) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            config = root / "dreadgoad.yaml"
            config.write_text("environments:\n  dev: {}\n")
            session = {"anchor": {"config_path": str(config), "env": "dev"}}

            async def fake_capture(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
                return 0, json.dumps(payload), ""

            try:
                await range_capabilities.load(
                    session, str(root), capture_command=fake_capture
                )
            except ValueError:
                return
            raise AssertionError(f"accepted invalid capability response: {payload}")

    asyncio.run(run({"commands": [{"name": "surprise", "supported": True}]}))
    asyncio.run(
        run(
            {
                "commands": [
                    {"name": "validate", "supported": True},
                    {"name": "validate", "supported": False},
                ]
            }
        )
    )


def test_load_rejects_invalid_agent_prompt() -> None:
    async def run(agent_prompt: object) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            config = root / "dreadgoad.yaml"
            config.write_text("environments:\n  dev: {}\n")
            session = {"anchor": {"config_path": str(config), "env": "dev"}}
            payload = {
                "agent_prompt": agent_prompt,
                "commands": [
                    {"name": name, "supported": name == "health"}
                    for name in ("health", "validate", "score", "reset", "scrub")
                ],
            }

            async def fake_capture(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
                return 0, json.dumps(payload), ""

            try:
                await range_capabilities.load(
                    session, str(root), capture_command=fake_capture
                )
            except ValueError:
                return
            raise AssertionError(f"accepted invalid agent prompt: {agent_prompt!r}")

    asyncio.run(run(42))
    asyncio.run(run("   "))
    asyncio.run(run("x" * (range_capabilities.MAX_AGENT_PROMPT_CHARS + 1)))


def test_variant_generating_command_invalidates_cached_context() -> None:
    async def run() -> None:
        session_id = "variant-context-test"
        current = chat_runtime.runtime(session_id)
        current.agent = object()
        current.agent_capabilities = range_capabilities.RangeContext(
            {"/health": {"name": "health", "supported": True}},
            "Source guidance.",
        )
        current.agent_commands = frozenset({"/health"})
        current.turn = chat_runtime.TurnState(started=True)

        async def fake_check(*_args, **_kwargs):  # noqa: ANN202
            return {}

        async def fake_emit(*_args, **_kwargs) -> None:
            return None

        async def fake_overlays(*_args, **_kwargs) -> None:
            return None

        original_check = hook.run_check
        original_emit = chat_events.emit_event
        original_overlays = command_runner._emit_overlays
        hook.run_check = fake_check
        chat_events.emit_event = fake_emit
        command_runner._emit_overlays = fake_overlays
        try:
            plan = command_runner._CommandPlan(
                "/up", ("dreadgoad", "up"), "/repo", commands.REGISTRY["/up"]
            )
            # A later /up stage may fail after generation completed, so a
            # non-zero normal completion must still invalidate the source root.
            await command_runner._finalize_command(
                SimpleNamespace(),
                session_id,
                plan,
                command_runner._RunResult(1, "provision failed", cancelled=False),
            )
            assert current.agent is None
            assert current.agent_capabilities is None
            assert current.agent_commands is None
            assert current.turn is not None and current.turn.started is True
        finally:
            hook.run_check = original_check
            chat_events.emit_event = original_emit
            command_runner._emit_overlays = original_overlays
            chat_runtime.runtimes.pop(session_id, None)

    asyncio.run(run())


def test_variant_context_invalidation_survives_refresh_failure() -> None:
    async def run() -> None:
        session_id = "variant-refresh-failure-test"
        current = chat_runtime.runtime(session_id)
        current.agent = object()
        current.agent_capabilities = range_capabilities.RangeContext({}, "Source.")
        current.agent_commands = frozenset({"/health"})

        async def failed_check(*_args, **_kwargs):  # noqa: ANN202
            raise RuntimeError("refresh failed")

        original_check = hook.run_check
        hook.run_check = failed_check
        try:
            plan = command_runner._CommandPlan(
                "/variant",
                ("dreadgoad", "variant"),
                "/repo",
                commands.REGISTRY["/variant"],
            )
            try:
                await command_runner._finalize_command(
                    SimpleNamespace(),
                    session_id,
                    plan,
                    command_runner._RunResult(0, "generated", cancelled=False),
                )
            except RuntimeError as exc:
                assert str(exc) == "refresh failed"
            else:
                raise AssertionError("post-command refresh failure was swallowed")
            assert current.agent is None
            assert current.agent_capabilities is None
            assert current.agent_commands is None
        finally:
            hook.run_check = original_check
            chat_runtime.runtimes.pop(session_id, None)

    asyncio.run(run())


def test_cancelled_variant_command_invalidates_cached_context() -> None:
    async def run() -> None:
        session_id = "variant-cancel-test"
        current = chat_runtime.runtime(session_id)
        current.agent = object()
        current.agent_capabilities = range_capabilities.RangeContext({}, "Source.")
        current.agent_commands = frozenset({"/health"})

        async def fake_check(*_args, **_kwargs):  # noqa: ANN202
            return {}

        async def fake_emit(*_args, **_kwargs) -> None:
            return None

        original_check = hook.run_check
        original_emit = chat_events.emit_event
        hook.run_check = fake_check
        chat_events.emit_event = fake_emit
        try:
            plan = command_runner._CommandPlan(
                "/up", ("dreadgoad", "up"), "/repo", commands.REGISTRY["/up"]
            )
            try:
                await command_runner._finalize_command(
                    SimpleNamespace(),
                    session_id,
                    plan,
                    command_runner._RunResult(130, "partial", cancelled=True),
                )
            except asyncio.CancelledError:
                pass
            else:
                raise AssertionError("cancelled command did not propagate cancellation")
            assert current.agent is None
            assert current.agent_capabilities is None
            assert current.agent_commands is None
        finally:
            hook.run_check = original_check
            chat_events.emit_event = original_emit
            chat_runtime.runtimes.pop(session_id, None)

    asyncio.run(run())
