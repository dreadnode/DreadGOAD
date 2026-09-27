"""Environment policy for CLI and cloud subprocesses.

The console process needs an LLM credential for agent turns, while the
``dreadgoad``/Terraform/Ansible process tree needs the operator's cloud and host
credentials. Build child environments by removing only the variables that
have explicitly been identified as LLM credentials; suffix-based filtering
would also remove required values such as ``AWS_SESSION_TOKEN``.
"""

from __future__ import annotations

import os

DEFAULT_LLM_SECRET_ENV = "OPENROUTER_API_KEY"
LLM_SECRET_ENV_SETTING = "DREADGOAD_CONSOLE_LLM_SECRET_ENV"

# These names pass the Settings route's credential-shaped-name validation, but
# they belong to infrastructure commands rather than the LLM. Treating one as
# an LLM key would silently remove a credential required by AWS descendants.
_INFRASTRUCTURE_CREDENTIAL_ENV_NAMES = frozenset(
    {
        "AWS_ACCESS_KEY_ID",
        "AWS_SECRET_ACCESS_KEY",
        "AWS_SESSION_TOKEN",
    }
)

_configured_secret_env = (
    os.environ.get(LLM_SECRET_ENV_SETTING, "").strip() or DEFAULT_LLM_SECRET_ENV
)
if _configured_secret_env in _INFRASTRUCTURE_CREDENTIAL_ENV_NAMES:
    raise RuntimeError(
        f"{LLM_SECRET_ENV_SETTING} cannot name infrastructure credential "
        f"{_configured_secret_env}"
    )

# Start with only the configured provider credential. Rebinding an immutable
# snapshot lets child_env() iterate safely if a Settings request registers
# another key name at the same time as a command is starting.
_llm_secret_env_names = frozenset({_configured_secret_env})
_active_llm_secret_env = _configured_secret_env


def register_llm_secret_env(name: str) -> None:
    """Remember an environment variable that must not reach child processes."""
    if not name:
        raise ValueError("LLM secret environment variable name cannot be empty")
    if is_infrastructure_credential_env(name):
        raise ValueError(f"{name} is reserved for infrastructure commands")
    global _active_llm_secret_env, _llm_secret_env_names
    _llm_secret_env_names = _llm_secret_env_names | {name}
    _active_llm_secret_env = name
    os.environ[LLM_SECRET_ENV_SETTING] = name


def active_llm_secret_env() -> str:
    """Return the provider-native credential variable selected most recently."""
    return _active_llm_secret_env


def is_infrastructure_credential_env(name: str) -> bool:
    """Return whether *name* must remain available to deployment commands."""
    return name in _INFRASTRUCTURE_CREDENTIAL_ENV_NAMES


def child_env() -> dict[str, str]:
    """Copy the server environment without registered LLM credentials."""
    env = os.environ.copy()
    for name in _llm_secret_env_names:
        env.pop(name, None)
    env.pop(LLM_SECRET_ENV_SETTING, None)
    return env
