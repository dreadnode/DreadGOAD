"""Tests for the command subprocess environment boundary.

Standalone:  python console/backend/tests/test_subprocess_env.py
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import subprocess
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.cli import capture, run_command  # noqa: E402
from console.backend.subprocess_env import (  # noqa: E402
    LLM_SECRET_ENV_SETTING,
    child_env,
    register_llm_secret_env,
)

_CUSTOM_LLM_KEY = "DREADGOAD_TEST_LLM_API_KEY"
_CLOUD_KEY = "AWS_SESSION_TOKEN"


def _set_test_environment() -> dict[str, str | None]:
    values = {
        "OPENROUTER_API_KEY": "openrouter-secret",
        _CUSTOM_LLM_KEY: "custom-secret",
        _CLOUD_KEY: "cloud-session-token",
    }
    previous = {name: os.environ.get(name) for name in values}
    os.environ.update(values)
    register_llm_secret_env(_CUSTOM_LLM_KEY)
    return previous


def _restore_environment(previous: dict[str, str | None]) -> None:
    for name, value in previous.items():
        if value is None:
            os.environ.pop(name, None)
        else:
            os.environ[name] = value


def _probe_argv() -> list[str]:
    names = ["OPENROUTER_API_KEY", _CUSTOM_LLM_KEY, _CLOUD_KEY]
    code = (
        "import json, os; "
        f"print(json.dumps({{name: os.environ.get(name) for name in {names!r}}}))"
    )
    return [sys.executable, "-c", code]


def test_child_env_scrubs_only_registered_llm_credentials() -> None:
    previous = _set_test_environment()
    try:
        env = child_env()
        assert "OPENROUTER_API_KEY" not in env
        assert _CUSTOM_LLM_KEY not in env
        assert env[_CLOUD_KEY] == "cloud-session-token"
    finally:
        _restore_environment(previous)


def test_launcher_scrubs_only_the_selected_provider_key() -> None:
    """Startup metadata scrubs the selected provider's native variable only."""
    provider_key = "DREADGOAD_TEST_PROVIDER_API_KEY"
    env = os.environ.copy()
    env[LLM_SECRET_ENV_SETTING] = provider_key
    env[provider_key] = "provider-secret"
    env["OPENROUTER_API_KEY"] = "openrouter-secret"
    code = (
        "import json, os; "
        "from console.backend.subprocess_env import "
        "active_llm_secret_env, child_env; "
        "child = child_env(); "
        f"print(json.dumps({{'active': active_llm_secret_env(), "
        f"'provider_value': os.environ.get({provider_key!r}), "
        f"'provider_in_child': {provider_key!r} in child, "
        "'openrouter_in_child': 'OPENROUTER_API_KEY' in child, "
        f"'metadata_in_child': {LLM_SECRET_ENV_SETTING!r} in child}}))"
    )
    result = subprocess.run(  # noqa: S603
        [sys.executable, "-c", code],
        cwd=pathlib.Path(__file__).resolve().parents[3],
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    values = json.loads(result.stdout)
    assert values == {
        "active": provider_key,
        "provider_value": "provider-secret",
        "provider_in_child": False,
        "openrouter_in_child": True,
        "metadata_in_child": False,
    }


def test_launcher_preserves_provider_native_key_name() -> None:
    """The shell launcher must pass policy metadata, not remap key values."""
    launcher = pathlib.Path(__file__).resolve().parents[3] / "dreadgoad-console"
    source = launcher.read_text(encoding="utf-8")
    assert 'export DREADGOAD_CONSOLE_LLM_SECRET_ENV="$API_KEY_ENV"' in source
    assert 'export OPENROUTER_API_KEY="$API_KEY_VALUE"' not in source
    assert source.count('export "$API_KEY_ENV=$LLM_API_KEY_VALUE"') == 1
    assert source.index('unset "$API_KEY_ENV"') < source.index(
        "# --- Python venv + backend deps"
    )


def test_launcher_rejects_infrastructure_credential_names() -> None:
    """AWS session-token aliases cannot be selected as LLM credentials."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    launcher = repo_root / "dreadgoad-console"
    for name in ("AWS_SECURITY_TOKEN", "AWS_SESSION_TOKEN"):
        result = subprocess.run(  # noqa: S603
            ["bash", str(launcher), "--api-key-env", name],
            cwd=repo_root,
            check=False,
            capture_output=True,
            text=True,
        )
        assert result.returncode == 2
        assert f"cannot use infrastructure credential {name}" in result.stderr


def test_launcher_scrubs_provider_key_before_build_helpers() -> None:
    """The real launcher prefix removes the key before dependency setup."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    launcher = repo_root / "dreadgoad-console"
    source = launcher.read_text(encoding="utf-8")
    prefix = source.split("# --- Python venv + backend deps", maxsplit=1)[0]
    probe = (
        "\nprintf 'VERIFY:%s:%s:%s\\n' "
        '"${ANTHROPIC_API_KEY-unset}" "$LLM_API_KEY_VALUE" '
        '"$DREADGOAD_CONSOLE_LLM_SECRET_ENV"\n'
        'python3 -c \'import os; print("CHILD:" + '
        'os.environ.get("LLM_API_KEY_VALUE", "unset"))\'\n'
    )
    env = os.environ.copy()
    env.update(
        {
            "ANTHROPIC_API_KEY": "anthropic-secret",
            "DREADGOAD_CONSOLE_PORT": "0",
            "LLM_API_KEY_VALUE": "attacker-seed",
            "NO_COLOR": "1",
        }
    )
    result = subprocess.run(  # noqa: S603
        [
            "bash",
            "-c",
            prefix + probe,
            "./dreadgoad-console",
            "--api-key-env",
            "ANTHROPIC_API_KEY",
        ],
        cwd=repo_root,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    assert result.stdout.splitlines()[-2:] == [
        "VERIFY:unset:anthropic-secret:ANTHROPIC_API_KEY",
        "CHILD:unset",
    ]


async def test_streaming_command_uses_scrubbed_environment() -> None:
    previous = _set_test_environment()
    try:
        rc, output = await run_command(_probe_argv(), cwd=".")
        values = json.loads(output)
        assert rc == 0
        assert values["OPENROUTER_API_KEY"] is None
        assert values[_CUSTOM_LLM_KEY] is None
        assert values[_CLOUD_KEY] == "cloud-session-token"
    finally:
        _restore_environment(previous)


async def test_captured_command_uses_scrubbed_environment() -> None:
    previous = _set_test_environment()
    try:
        rc, stdout, stderr = await capture(_probe_argv(), cwd=".")
        values = json.loads(stdout)
        assert rc == 0 and not stderr
        assert values["OPENROUTER_API_KEY"] is None
        assert values[_CUSTOM_LLM_KEY] is None
        assert values[_CLOUD_KEY] == "cloud-session-token"
    finally:
        _restore_environment(previous)


def main() -> None:
    test_child_env_scrubs_only_registered_llm_credentials()
    test_launcher_scrubs_only_the_selected_provider_key()
    test_launcher_preserves_provider_native_key_name()
    test_launcher_rejects_infrastructure_credential_names()
    test_launcher_scrubs_provider_key_before_build_helpers()
    asyncio.run(test_streaming_command_uses_scrubbed_environment())
    asyncio.run(test_captured_command_uses_scrubbed_environment())
    print("ALL PASS")


if __name__ == "__main__":
    main()
