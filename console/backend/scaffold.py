"""Build the infrastructure an environment needs, via ``dreadgoad env create``.

Writing an environment into ``dreadgoad.yaml`` is only half of creating one. The
CLI resolves terragrunt out of ``infra/<provider>/<deployment>/<env>/<region>/``
and nothing creates that tree implicitly, so a config-only environment produces a
session that looks healthy and then fails at the first ``/up``. That is exactly
what happened to the first environment created through the console.

``dreadgoad env create`` already scaffolds all of it — env.hcl with derived
subnet CIDRs, region.hcl, a copy of the reference region's modules, the variant,
and the inventory — so this shells out to it rather than reimplementing the
layout in Python, the same way lab discovery defers to ``lab list --json``.
"""

from __future__ import annotations

import os
import typing as t

import logging

from . import commands, labconfig, paths, projectroot
from .cli import Capture, capture

log = logging.getLogger(__name__)

# Mirrors viper's default (cli/internal/config/defaults.go:123). The console
# writes configs without an `infra:` block, so this is what they resolve to.
DEFAULT_DEPLOYMENT = "goad-deployment"


def infra_env_dir(
    project_root: str, provider: str, env: str, deployment: str = DEFAULT_DEPLOYMENT
) -> str:
    """Where the CLI will look for this environment's terragrunt tree.

    Azure nests under ``infra/azure/<deployment>``; every other provider uses
    ``infra/<deployment>`` (config.go:504-511).
    """
    if provider == "azure":
        return os.path.join(project_root, "infra", "azure", deployment, env)
    return os.path.join(project_root, "infra", deployment, env)


def preflight(
    project_root: str,
    provider: str,
    env: str,
    variant_target: str | None,
    deployment: str = DEFAULT_DEPLOYMENT,
) -> list[str]:
    """Blocking reasons ``env create`` would fail, or [] if it should succeed.

    Checked here rather than left to the CLI because its failure mode is not
    recoverable from the UI: a run that dies partway leaves the infra directory
    behind, and re-running then fails on *that* — while ``--force``, which skips
    the infra check, still refuses the existing variant target. The operator
    ends up needing to delete two directories by hand, having been told only
    about one. Refusing before anything is written keeps that state unreachable.
    """
    problems: list[str] = []

    env_dir = infra_env_dir(project_root, provider, env, deployment)
    if os.path.exists(env_dir):
        problems.append(
            f"{env_dir} already exists — an environment of this name already has "
            f"infrastructure. Pick another name, or remove that directory."
        )

    if variant_target:
        target = variant_target
        if not os.path.isabs(target):
            target = os.path.join(project_root, target)
        if os.path.exists(target):
            problems.append(
                f"{target} already exists — the variant generator refuses to "
                f"overwrite it. Pick another name, or remove that directory."
            )

    return problems


def build_argv(
    config_path: str,
    env: str,
    region: str,
    *,
    variant: bool = False,
    variant_source: str | None = None,
    vpc_cidr: str | None = None,
) -> list[str]:
    """The ``env create`` invocation for one environment."""
    argv = [
        commands.resolve_bin(str(paths.repo_root())),
        "--config",
        str(config_path),
        "--env",
        env,
        "env",
        "create",
        env,
        "--region",
        region,
    ]
    if vpc_cidr:
        # Passed explicitly even though VpcCIDR would find it in the config we
        # just wrote (config.go:438-441): the CIDR ends up baked into env.hcl's
        # subnet math, and having the console state it leaves no chance of the
        # file and the terraform disagreeing.
        argv += ["--vpc-cidr", vpc_cidr]
    if variant:
        argv.append("--variant")
        if variant_source:
            argv += ["--variant-source", variant_source]
    return argv


async def scaffold_env(
    config_path: str,
    env: str,
    region: str,
    *,
    variant: bool = False,
    variant_source: str | None = None,
    variant_target: str | None = None,
    vpc_cidr: str | None = None,
    provider: str = "",
    capture_command: Capture | None = None,
) -> tuple[bool, str]:
    """Scaffold ``env``'s infrastructure. Returns (ok, combined output).

    Runs in the config's own tree, like every other spawn (projectroot.run_cwd),
    so a config in another checkout scaffolds into that checkout rather than the
    console's.
    """
    if not str(region).strip():
        # env create would be handed `--region ""` and fall back to
        # ResolveRegion, which fails with a message about the CLI rather than
        # about what the console did. SessionService checks this too; keeping it
        # here means the module cannot be misused into that state.
        return False, "no region set for this environment; env create requires one"

    root, _ = projectroot.resolve_root(config_path)
    problems = preflight(str(root), provider, env, variant_target)
    if problems:
        return False, "\n".join(problems)

    argv = build_argv(
        config_path,
        env,
        region,
        variant=variant,
        variant_source=variant_source,
        vpc_cidr=vpc_cidr,
    )
    runner = capture_command or capture
    try:
        return_code, stdout, stderr = await runner(argv, str(root))
    except (OSError, ValueError) as exc:
        # Same reasoning as labs.discover_labs: a missing binary raises from
        # create_subprocess_exec rather than returning non-zero.
        return False, f"could not run dreadgoad env create: {exc}"

    output = (stdout or "") + (stderr or "")
    return return_code == 0, output.strip()


async def generate_answer_key(
    session: dict[str, t.Any],
    fallback_root: str,
    capture_command: Capture | None = None,
) -> str | None:
    """Generate ``answer_key.json`` beside a session's lab config.

    Returns the output path on success, or None on failure (logged, not raised).
    Called after the variant is scaffolded so the config.json exists.
    """
    config_path = labconfig.session_lab_config_path(session, fallback_root)
    if not config_path or not os.path.isfile(config_path):
        return None

    output_path = os.path.join(os.path.dirname(config_path), "answer_key.json")
    if os.path.isfile(output_path):
        return output_path

    anchor = session.get("anchor") or {}
    cp = anchor.get("config_path")
    if not cp:
        return None
    root = str(projectroot.resolve_root(cp)[0])

    argv = [
        commands.resolve_bin(root),
        "--config",
        str(cp),
        "--env",
        str(anchor.get("env", "")),
        "score",
        "generate-key",
        "--config",
        config_path,
        "--output",
        output_path,
    ]
    runner = capture_command or capture
    try:
        rc, stdout, stderr = await runner(argv, root)
    except (OSError, ValueError) as exc:
        log.warning("answer key generation failed: %s", exc)
        return None

    if rc != 0:
        log.warning(
            "answer key generation exited %d: %s", rc, (stderr or stdout or "").strip()
        )
        return None

    log.info("generated answer key: %s", output_path)
    return output_path
