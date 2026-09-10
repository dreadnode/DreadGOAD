"""Tests for generic, range-owned console session initialization."""

from __future__ import annotations

import asyncio
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import lifecycle  # noqa: E402
from console.backend.schemas import SessionDocument  # noqa: E402


def _session(root: pathlib.Path) -> SessionDocument:
    return {
        "id": "s-test",
        "label": "test",
        "status": "new",
        "anchor": {"config_path": str(root / "dreadgoad.yaml"), "env": "dev"},
        "snapshot": {
            "provider": None,
            "region": None,
            "lab": None,
            "variant_name": None,
            "vpc_cidr": None,
            "attack_box": None,
        },
        "session_dir": str(root / "sessions" / "s-test"),
        "created_at": "now",
        "updated_at": "now",
    }


async def test_initializer_uses_one_generic_cli_command() -> None:
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d)
        (root / "ansible").mkdir()
        (root / "dreadgoad.yaml").write_text("env: dev\n")
        session = _session(root)
        seen: list[tuple[list[str], str]] = []

        async def capture(argv: list[str], cwd: str) -> tuple[int, str, str]:
            seen.append((argv, cwd))
            return (
                0,
                '{"actions":[{"action":"generate_answer_key",'
                '"status":"completed","artifacts":["/key"]}]}',
                "",
            )

        results = await lifecycle.initialize_session(
            session, str(root), capture_command=capture
        )
        assert results == [
            {
                "action": "generate_answer_key",
                "status": "completed",
                "artifacts": ["/key"],
            }
        ]
        argv, cwd = seen[0]
        assert cwd == str(root.resolve())
        assert argv[-5:] == [
            "range",
            "init-session",
            "--output-dir",
            str(root / "sessions" / "s-test" / "artifacts"),
            "--json",
        ]


def test_result_parser_rejects_malformed_entries() -> None:
    assert lifecycle._parse_results("not json") is None
    assert (
        lifecycle._parse_results('{"actions":[{"action":7,"status":"done"}]}') is None
    )
    assert lifecycle._parse_results('{"actions":[]}') == []


async def test_failed_spawn_becomes_a_nonfatal_result() -> None:
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d)
        (root / "ansible").mkdir()
        (root / "dreadgoad.yaml").write_text("env: dev\n")

        async def missing(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
            raise FileNotFoundError("missing CLI")

        results = await lifecycle.initialize_session(
            _session(root), str(root), capture_command=missing
        )
        assert len(results) == 1 and results[0]["status"] == "failed"


async def test_nonzero_empty_response_is_not_silently_treated_as_noop() -> None:
    with tempfile.TemporaryDirectory() as d:
        root = pathlib.Path(d)
        (root / "ansible").mkdir()
        (root / "dreadgoad.yaml").write_text("env: dev\n")

        async def rejected(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
            return 1, '{"actions":[]}', "unsupported session_init action"

        results = await lifecycle.initialize_session(
            _session(root), str(root), capture_command=rejected
        )
        assert len(results) == 1 and results[0]["status"] == "failed"
        assert "unsupported" in results[0].get("message", "")


async def _main() -> None:
    await test_initializer_uses_one_generic_cli_command()
    test_result_parser_rejects_malformed_entries()
    await test_failed_spawn_becomes_a_nonfatal_result()
    await test_nonzero_empty_response_is_not_silently_treated_as_noop()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
