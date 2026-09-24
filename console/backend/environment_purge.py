"""Safely remove console-owned files after a successful infrastructure destroy."""

from __future__ import annotations

import os
import re
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path

import yaml

from . import paths, projectroot, scaffold


_SAFE_ENV = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")


@dataclass(frozen=True)
class PurgePlan:
    """Validated, console-owned paths that one purge may remove."""

    env: str
    project_root: Path
    config_path: Path
    targets: tuple[Path, ...]


def _inside(path: Path, root: Path) -> bool:
    """Return whether path resolves below root, never equal to root itself."""
    resolved = path.resolve(strict=False)
    base = root.resolve(strict=False)
    return resolved != base and base in resolved.parents


def _symlink_component(path: Path, root: Path) -> Path | None:
    """Return the first symlink at or below root on path's lexical route."""
    lexical_root = Path(os.path.abspath(root))
    lexical_path = Path(os.path.abspath(path))
    try:
        relative = lexical_path.relative_to(lexical_root)
    except ValueError:
        return lexical_path
    current = lexical_root
    for part in relative.parts:
        current /= part
        if current.is_symlink():
            return current
    return None


def _environment_settings(config_path: Path, env: str) -> dict[str, object]:
    with config_path.open(encoding="utf-8") as handle:
        document = yaml.safe_load(handle) or {}
    if not isinstance(document, dict):
        raise ValueError(f"{config_path} is not a YAML mapping")
    environments = document.get("environments")
    if not isinstance(environments, dict) or env not in environments:
        raise ValueError(f"environment {env!r} is not present in {config_path}")
    if set(environments) != {env}:
        raise ValueError(
            "--purge requires a console-managed config containing only this "
            "environment; use /destroy without --purge for shared configs"
        )
    settings = environments[env] or {}
    if not isinstance(settings, dict):
        raise ValueError(f"environment {env!r} in {config_path} is not a mapping")
    return settings


def build_plan(session: dict[str, object]) -> PurgePlan:
    """Validate ownership and enumerate artifacts before cloud destruction starts."""
    anchor = session.get("anchor")
    snapshot = session.get("snapshot")
    if not isinstance(anchor, dict) or not isinstance(snapshot, dict):
        raise ValueError("session is missing its environment anchor")

    env = str(anchor.get("env") or "")
    if not _SAFE_ENV.fullmatch(env):
        raise ValueError(f"refusing to purge unsafe environment name {env!r}")

    raw_config = str(anchor.get("config_path") or "")
    if not raw_config:
        raise ValueError("session has no config path")
    config_path = Path(raw_config).expanduser().resolve(strict=False)
    managed_root = paths.configs_root().resolve(strict=False)
    if not _inside(config_path, managed_root):
        raise ValueError(
            "--purge is limited to console-managed configs; this session uses "
            f"{config_path}"
        )

    settings = _environment_settings(config_path, env)
    project_root, _ = projectroot.resolve_root(config_path)
    project_root = project_root.resolve(strict=False)
    provider = str(snapshot.get("provider") or "")
    deployment = str(snapshot.get("deployment") or scaffold.DEFAULT_DEPLOYMENT)
    if not _SAFE_ENV.fullmatch(deployment):
        raise ValueError(f"refusing to purge unsafe deployment name {deployment!r}")

    candidates = [
        Path(scaffold.infra_env_dir(str(project_root), provider, env, deployment)),
        project_root / f"{env}-inventory",
        project_root / ".dreadgoad" / "cache" / f"{env}-config.json",
    ]

    is_variant = bool(settings.get("variant"))
    source: str | None = None
    variant_target: Path | None = None
    if is_variant:
        source = str(settings.get("variant_source") or "ad/GOAD")
        expected_target = project_root / "ad" / f"{Path(source).name}-{env}"
        raw_target = str(settings.get("variant_target") or expected_target)
        variant_target = Path(raw_target)
        if not variant_target.is_absolute():
            variant_target = project_root / variant_target
        if variant_target.resolve(strict=False) != expected_target.resolve(
            strict=False
        ):
            raise ValueError(
                "refusing to purge variant_target that is not the generated "
                f"target for this environment: {variant_target}"
            )
        candidates.append(variant_target)
    else:
        lab = str(snapshot.get("lab") or "")
        if lab == "GOAD" or Path(lab).parts == ("ad", "GOAD"):
            data_dir = project_root / "ad" / "GOAD" / "data"
            candidates.extend(
                [
                    data_dir / f"{env}-overlay.json",
                    data_dir / f"{env}-config.json",
                ]
            )

    ownership_marker = scaffold.require_ownership(
        config_path,
        env,
        project_root,
        provider,
        deployment,
        variant=is_variant,
        variant_source=source,
        variant_target=variant_target,
    )

    for candidate in candidates:
        if not _inside(candidate, project_root):
            raise ValueError(f"refusing to purge path outside project: {candidate}")
        if symlink := _symlink_component(candidate, project_root):
            raise ValueError(f"refusing to purge symlinked path: {symlink}")
    if not _inside(ownership_marker, managed_root):
        raise ValueError(
            f"refusing to purge ownership marker outside managed configs: {ownership_marker}"
        )
    if symlink := _symlink_component(ownership_marker, managed_root):
        raise ValueError(f"refusing to purge symlinked path: {symlink}")

    # The managed config is moved last. If an earlier move fails, rollback can
    # restore every path and leave the still-running session usable.
    targets = tuple(
        dict.fromkeys(
            [
                *(Path(os.path.abspath(path)) for path in candidates),
                ownership_marker,
                config_path,
            ]
        )
    )
    return PurgePlan(
        env=env,
        project_root=project_root,
        config_path=config_path,
        targets=targets,
    )


def execute(plan: PurgePlan) -> list[str]:
    """Atomically detach purge targets, rolling back if any move fails."""
    staging = Path(
        tempfile.mkdtemp(prefix=f".dreadgoad-purge-{plan.env}-", dir=plan.project_root)
    )
    moved: list[tuple[Path, Path]] = []
    managed_root = paths.configs_root().resolve(strict=False)
    managed_targets = {
        plan.config_path,
        scaffold.ownership_marker_path(plan.config_path, plan.env),
    }
    try:
        for index, original in enumerate(plan.targets):
            if not original.exists() and not original.is_symlink():
                continue
            target_root = (
                managed_root if original in managed_targets else plan.project_root
            )
            if not _inside(original, target_root):
                raise RuntimeError(
                    f"refusing to purge path outside its root: {original}"
                )
            if symlink := _symlink_component(original, target_root):
                raise RuntimeError(f"refusing to purge symlinked path: {symlink}")
            destination = staging / f"{index}-{original.name}"
            os.replace(original, destination)
            moved.append((original, destination))
    except Exception:
        for original, destination in reversed(moved):
            if destination.exists() or destination.is_symlink():
                os.replace(destination, original)
        shutil.rmtree(staging, ignore_errors=True)
        raise

    removed = [str(original) for original, _ in moved]
    try:
        shutil.rmtree(staging)
    except OSError as exc:
        # Names are already free for recreation. Surface the quarantined path
        # so the operator can recover disk space without guessing what remains.
        raise RuntimeError(
            f"environment paths were detached, but cleanup of {staging} failed: {exc}"
        ) from exc
    return removed
