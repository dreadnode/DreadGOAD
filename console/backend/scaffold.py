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

import hashlib
import json
import os
import stat
import tempfile
from pathlib import Path

from . import commands, paths, projectroot
from .cli import Capture, capture

# Mirrors viper's default (cli/internal/config/defaults.go:123). The console
# writes configs without an `infra:` block, so this is what they resolve to.
DEFAULT_DEPLOYMENT = "goad-deployment"
_OWNERSHIP_VERSION = 2


def ownership_marker_path(config_path: str | Path, env: str) -> Path:
    """Return the private marker proving a managed scaffold completed."""
    config = Path(config_path).expanduser().resolve(strict=False)
    digest = hashlib.sha256(env.encode("utf-8")).hexdigest()[:16]
    return config.with_name(f".{config.name}.{digest}.scaffold.json")


def _variant_ownership(
    env: str,
    project_root: str | Path,
    variant: bool,
    variant_source: str | Path | None,
    variant_target: str | Path | None,
) -> dict[str, object]:
    root = Path(project_root).resolve(strict=False)
    if not variant:
        return {"variant": False, "variant_source": None, "variant_target": None}

    source = Path(variant_source or "ad/GOAD")
    if not source.is_absolute():
        source = root / source
    source = Path(os.path.abspath(source))
    target = Path(variant_target or (root / "ad" / f"{source.name}-{env}"))
    if not target.is_absolute():
        target = root / target
    target = Path(os.path.abspath(target))
    return {
        "variant": True,
        "variant_source": str(source),
        "variant_target": str(target),
    }


def record_ownership(
    config_path: str | Path,
    env: str,
    project_root: str | Path,
    provider: str,
    deployment: str,
    *,
    variant: bool = False,
    variant_source: str | Path | None = None,
    variant_target: str | Path | None = None,
) -> Path | None:
    """Persist proof that this console successfully scaffolded an environment."""
    config = Path(config_path).expanduser().resolve(strict=False)
    managed_root = paths.configs_root().resolve(strict=False)
    if config.parent != managed_root:
        return None

    marker = ownership_marker_path(config, env)
    payload = {
        "version": _OWNERSHIP_VERSION,
        "config_path": str(config),
        "env": env,
        "project_root": str(Path(project_root).resolve(strict=False)),
        "provider": provider,
        "deployment": deployment,
        **_variant_ownership(
            env, project_root, variant, variant_source, variant_target
        ),
    }
    fd, temporary = tempfile.mkstemp(prefix=f".{marker.name}.", dir=marker.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            json.dump(payload, handle, sort_keys=True)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, marker)
    except Exception:
        try:
            os.unlink(temporary)
        except OSError:
            pass
        raise
    return marker


def require_ownership(
    config_path: str | Path,
    env: str,
    project_root: str | Path,
    provider: str,
    deployment: str,
    *,
    variant: bool = False,
    variant_source: str | Path | None = None,
    variant_target: str | Path | None = None,
) -> Path:
    """Validate and return the scaffold ownership marker for one environment."""
    config = Path(config_path).expanduser().resolve(strict=False)
    root = Path(project_root).resolve(strict=False)
    marker = ownership_marker_path(config, env)
    try:
        mode = marker.lstat().st_mode
    except FileNotFoundError as exc:
        raise ValueError(
            "refusing to purge project artifacts without a console scaffold "
            f"ownership marker: {marker}"
        ) from exc
    if not stat.S_ISREG(mode) or marker.is_symlink():
        raise ValueError(f"refusing invalid scaffold ownership marker: {marker}")
    try:
        payload = json.loads(marker.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"invalid scaffold ownership marker {marker}: {exc}") from exc
    expected = {
        "version": _OWNERSHIP_VERSION,
        "config_path": str(config),
        "env": env,
        "project_root": str(root),
        "provider": provider,
        "deployment": deployment,
        **_variant_ownership(
            env, project_root, variant, variant_source, variant_target
        ),
    }
    if payload != expected:
        raise ValueError(
            f"scaffold ownership marker does not match this environment: {marker}"
        )
    return marker


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

    inventory = os.path.join(project_root, f"{env}-inventory")
    if os.path.exists(inventory):
        problems.append(
            f"{inventory} already exists — environment names must be unique "
            f"within a checkout. Pick another name, or remove that inventory."
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
    deployment: str = DEFAULT_DEPLOYMENT,
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
    problems = preflight(
        str(root), provider, env, variant_target, deployment=deployment
    )
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
    if return_code == 0:
        try:
            record_ownership(
                config_path,
                env,
                root,
                provider,
                deployment,
                variant=variant,
                variant_source=variant_source,
                variant_target=variant_target,
            )
        except OSError as exc:
            output += (
                "\nWarning: infrastructure was scaffolded, but console ownership "
                f"could not be recorded; /destroy --purge will be unavailable: {exc}"
            )
    return return_code == 0, output.strip()
