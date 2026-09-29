"""Tests for the command subprocess environment boundary.

Standalone:  python console/backend/tests/test_subprocess_env.py
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import shlex
import subprocess
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.cli import capture, run_command  # noqa: E402
from console.backend.subprocess_env import (  # noqa: E402
    LLM_SECRET_ENV_SETTING,
    LLM_SECRET_ENVS_SETTING,
    child_env,
    register_llm_secret_env,
)

_CUSTOM_LLM_KEY = "DREADGOAD_TEST_LLM_API_KEY"
_CLOUD_KEY = "AWS_SESSION_TOKEN"
_DEPLOYMENT_API_KEY = "LUDUS_API_KEY"


def _set_test_environment() -> dict[str, str | None]:
    values = {
        "OPENROUTER_API_KEY": "openrouter-secret",
        _CUSTOM_LLM_KEY: "custom-secret",
        _CLOUD_KEY: "cloud-session-token",
        _DEPLOYMENT_API_KEY: "deployment-api-key",
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
    names = [
        "OPENROUTER_API_KEY",
        _CUSTOM_LLM_KEY,
        _CLOUD_KEY,
        _DEPLOYMENT_API_KEY,
    ]
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
        assert env[_DEPLOYMENT_API_KEY] == "deployment-api-key"
    finally:
        _restore_environment(previous)


def test_backend_scrubs_all_launcher_registered_provider_keys() -> None:
    """Startup metadata registers known and custom selected provider keys."""
    provider_key = "DREADGOAD_TEST_PROVIDER_API_KEY"
    registered_keys = (
        "OPENROUTER_API_KEY",
        "ANTHROPIC_API_KEY",
        "OPENAI_API_KEY",
        provider_key,
    )
    env = os.environ.copy()
    env[LLM_SECRET_ENV_SETTING] = provider_key
    env[LLM_SECRET_ENVS_SETTING] = ",".join(registered_keys)
    env.update({name: f"{name.lower()}-secret" for name in registered_keys})
    code = (
        "import json, os; "
        "from console.backend.subprocess_env import "
        "active_llm_secret_env, child_env; "
        "child = child_env(); "
        f"names = {registered_keys!r}; "
        "print(json.dumps({'active': active_llm_secret_env(), "
        "'backend_values': {name: os.environ.get(name) for name in names}, "
        "'child_values': {name: child.get(name) for name in names}, "
        f"'metadata_in_child': {LLM_SECRET_ENV_SETTING!r} in child, "
        f"'metadata_list_in_child': {LLM_SECRET_ENVS_SETTING!r} in child}}))"
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
    assert values["active"] == provider_key
    assert all(values["backend_values"].values())
    assert values["child_values"] == dict.fromkeys(registered_keys)
    assert values["metadata_in_child"] is False
    assert values["metadata_list_in_child"] is False


def test_launcher_preserves_provider_native_key_name() -> None:
    """The shell launcher must pass policy metadata, not remap key values."""
    launcher = pathlib.Path(__file__).resolve().parents[3] / "dreadgoad-console"
    source = launcher.read_text(encoding="utf-8")
    assert 'export DREADGOAD_CONSOLE_LLM_SECRET_ENV="$API_KEY_ENV"' in source
    assert 'export DREADGOAD_CONSOLE_LLM_SECRET_ENVS="$LLM_SECRET_ENV_LIST"' in source
    assert 'export OPENROUTER_API_KEY="$API_KEY_VALUE"' not in source
    assert '"OPENROUTER_API_KEY"' in source
    assert '"ANTHROPIC_API_KEY"' in source
    assert '"OPENAI_API_KEY"' in source
    assert 'export "$API_KEY_ENV=$LLM_API_KEY_VALUE"' in source
    assert source.index('unset "$secret_name"') < source.index(
        "# --- Python venv + backend deps"
    )


def test_launcher_help_runs_without_external_tools() -> None:
    """Even early help rendering must not expose credentials to a child tool."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    launcher = repo_root / "dreadgoad-console"
    env = os.environ.copy()
    env.update(
        {
            "PATH": "",
            "OPENROUTER_API_KEY": "openrouter-secret",
            "ANTHROPIC_API_KEY": "anthropic-secret",
        }
    )
    result = subprocess.run(  # noqa: S603
        ["/bin/bash", str(launcher), "--api-key-env", "ANTHROPIC_API_KEY", "--help"],
        cwd=repo_root,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    assert "USAGE" in result.stdout


def test_launcher_restores_only_selected_key_for_backend() -> None:
    """The real start_backend function restores only the active provider key."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    source = (repo_root / "dreadgoad-console").read_text(encoding="utf-8")
    start = source.index("start_backend() {")
    end = source.index('\n}\n\nif [ "$DEV"', start) + 2
    function_source = source[start:end]

    with tempfile.TemporaryDirectory() as temp_dir:
        venv_bin = pathlib.Path(temp_dir) / "bin"
        venv_bin.mkdir()
        fake_uvicorn = venv_bin / "uvicorn"
        fake_uvicorn.write_text(
            "#!/usr/bin/env python3\n"
            "import json, os\n"
            'names = ["OPENROUTER_API_KEY", "ANTHROPIC_API_KEY", '
            '"OPENAI_API_KEY", '
            '"DREADGOAD_TEST_PROVIDER_API_KEY", '
            '"DREADGOAD_CONSOLE_AUTH_TOKEN", '
            '"LLM_API_KEY_VALUE"]\n'
            "print(json.dumps({name: os.environ.get(name) for name in names}))\n",
            encoding="utf-8",
        )
        fake_uvicorn.chmod(0o755)
        cases = (
            (
                "default selected",
                "OPENROUTER_API_KEY",
                "openrouter-secret",
            ),
            (
                "custom selected",
                "DREADGOAD_TEST_PROVIDER_API_KEY",
                "provider-secret",
            ),
        )
        for label, selected_name, selected_value in cases:
            script = (
                f"{function_source}\n"
                f"API_KEY_ENV={shlex.quote(selected_name)}\n"
                f"LLM_API_KEY_VALUE={shlex.quote(selected_value)}\n"
                f"VENV={shlex.quote(temp_dir)}\n"
                'AUTH_TOKEN="browser-token"\n'
                "start_backend console.backend.server:app\n"
            )
            env = os.environ.copy()
            for name in (
                "OPENROUTER_API_KEY",
                "ANTHROPIC_API_KEY",
                "OPENAI_API_KEY",
                "DREADGOAD_TEST_PROVIDER_API_KEY",
            ):
                env.pop(name, None)
            env.update(
                {
                    "LLM_API_KEY_VALUE": "attacker-seed",
                }
            )
            result = subprocess.run(  # noqa: S603
                ["bash", "-c", script],
                cwd=repo_root,
                env=env,
                check=True,
                capture_output=True,
                text=True,
            )
            values = json.loads(result.stdout)
            expected = {
                "OPENROUTER_API_KEY": None,
                "ANTHROPIC_API_KEY": None,
                "OPENAI_API_KEY": None,
                "DREADGOAD_TEST_PROVIDER_API_KEY": None,
                "DREADGOAD_CONSOLE_AUTH_TOKEN": "browser-token",
                "LLM_API_KEY_VALUE": None,
            }
            expected[selected_name] = selected_value
            assert values == expected, label


def test_launcher_rejects_infrastructure_credential_names() -> None:
    """Deployment credentials cannot be selected as LLM credentials."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    launcher = repo_root / "dreadgoad-console"
    for name in (
        "ARM_OIDC_TOKEN",
        "AWS_CONTAINER_AUTHORIZATION_TOKEN",
        "AWS_SECURITY_TOKEN",
        "AWS_SESSION_TOKEN",
        "LUDUS_API_KEY",
    ):
        result = subprocess.run(  # noqa: S603
            ["bash", str(launcher), "--api-key-env", name],
            cwd=repo_root,
            check=False,
            capture_output=True,
            text=True,
        )
        assert result.returncode == 2
        assert f"cannot use infrastructure credential {name}" in result.stderr


def test_backend_rejects_infrastructure_credential_metadata() -> None:
    """Launcher metadata cannot classify cloud credentials as LLM secrets."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    for name in ("ARM_OIDC_TOKEN", "AWS_SESSION_TOKEN", "LUDUS_API_KEY"):
        env = os.environ.copy()
        env[LLM_SECRET_ENVS_SETTING] = f"OPENROUTER_API_KEY,{name}"
        result = subprocess.run(  # noqa: S603
            [sys.executable, "-c", "import console.backend.subprocess_env"],
            cwd=repo_root,
            env=env,
            check=False,
            capture_output=True,
            text=True,
        )
        assert result.returncode != 0
        assert f"cannot name infrastructure credential {name}" in result.stderr


def test_launcher_scrubs_provider_key_before_build_helpers() -> None:
    """The real launcher prefix removes the key before dependency setup."""
    repo_root = pathlib.Path(__file__).resolve().parents[3]
    launcher = repo_root / "dreadgoad-console"
    source = launcher.read_text(encoding="utf-8")
    prefix = source.split("# --- Python venv + backend deps", maxsplit=1)[0]
    probe = (
        "\nprintf 'VERIFY:%s:%s:%s:%s:%s:%s:%s\\n' "
        '"${DREADGOAD_TEST_PROVIDER_API_KEY-unset}" '
        '"${ANTHROPIC_API_KEY-unset}" "${OPENROUTER_API_KEY-unset}" '
        '"${OPENAI_API_KEY-unset}" '
        '"$LLM_API_KEY_VALUE" "$DREADGOAD_CONSOLE_LLM_SECRET_ENV" '
        '"$DREADGOAD_CONSOLE_LLM_SECRET_ENVS"\n'
        'python3 -c \'import os; print("CHILD:%s:%s:%s:%s:%s" % '
        '(os.environ.get("DREADGOAD_TEST_PROVIDER_API_KEY", "unset"), '
        'os.environ.get("ANTHROPIC_API_KEY", "unset"), '
        'os.environ.get("OPENROUTER_API_KEY", "unset"), '
        'os.environ.get("OPENAI_API_KEY", "unset"), '
        'os.environ.get("LLM_API_KEY_VALUE", "unset")))\'\n'
    )
    env = os.environ.copy()
    env.update(
        {
            "ANTHROPIC_API_KEY": "anthropic-secret",
            "DREADGOAD_TEST_PROVIDER_API_KEY": "provider-secret",
            "OPENAI_API_KEY": "openai-secret",
            "OPENROUTER_API_KEY": "openrouter-secret",
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
            "DREADGOAD_TEST_PROVIDER_API_KEY",
        ],
        cwd=repo_root,
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    assert result.stdout.splitlines()[-2:] == [
        "VERIFY:unset:unset:unset:unset:provider-secret:"
        "DREADGOAD_TEST_PROVIDER_API_KEY:"
        "OPENROUTER_API_KEY,ANTHROPIC_API_KEY,OPENAI_API_KEY,"
        "DREADGOAD_TEST_PROVIDER_API_KEY",
        "CHILD:unset:unset:unset:unset:unset",
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
    test_backend_scrubs_all_launcher_registered_provider_keys()
    test_launcher_preserves_provider_native_key_name()
    test_launcher_help_runs_without_external_tools()
    test_launcher_restores_only_selected_key_for_backend()
    test_launcher_rejects_infrastructure_credential_names()
    test_backend_rejects_infrastructure_credential_metadata()
    test_launcher_scrubs_provider_key_before_build_helpers()
    asyncio.run(test_streaming_command_uses_scrubbed_environment())
    asyncio.run(test_captured_command_uses_scrubbed_environment())
    print("ALL PASS")


if __name__ == "__main__":
    main()
