"""Where the CLI will look for a range's files, and whether they are there.

The dreadgoad CLI takes ``--config`` for the config file, but resolves every
*other* path from a "project root" it infers by walking up from its working
directory looking for an ``ansible/`` dir (cli/internal/config/config.go,
findProjectRoot). Inventory, lab data, ansible.cfg and the cache all hang off
that root:

    <root>/<env>-inventory            config.go:192
    <root>/ad/GOAD/data               config.go:247
    <root>/ansible/ansible.cfg        config.go:308
    <root>/.dreadgoad/cache           config.go:254

So the config path and the working directory are two independent inputs, and
the console used to supply a fixed working directory (its own repo) regardless
of where the config lived. Pointing the console at a config in another checkout
therefore resolved the range's files into the console's tree, where they do not
exist — an operator saw 22 identical "inventory not found" failures, twenty-five
minutes after asking, for a file that was sitting next to their config the whole
time.

Running the CLI in the config's own directory makes the inference land where the
operator would have landed running it by hand. This module computes that
directory and reports what is missing, so the answer arrives before the spawn
rather than after a full sweep of timeouts.
"""

from __future__ import annotations

import typing as t
from dataclasses import dataclass, field
from pathlib import Path

# The directory whose presence marks a project root. Must match the Go walk
# (config.go:506) — if that marker changes, this silently starts disagreeing
# with the CLI it exists to predict.
ROOT_MARKER = "ansible"


@dataclass(frozen=True)
class Preflight:
    """What the CLI will resolve for a session, and what is missing."""

    root: Path
    """Directory the CLI will treat as the project root."""

    marker_found: bool
    """True if ROOT_MARKER was found. False means the walk fell back, so the
    root is a guess and every derived path is suspect."""

    inventory: Path
    """Where ``<env>-inventory`` is expected."""

    warnings: list[str] = field(default_factory=list)
    """Operator-facing problems, most specific first. Empty means all good."""


def resolve_root(config_path: str | Path) -> tuple[Path, bool]:
    """Find the project root for ``config_path``; mirrors the CLI's own walk.

    Starts at the config's directory and walks up looking for ``ansible/``.
    Returns ``(root, marker_found)``. When nothing is found the starting
    directory is returned with ``marker_found=False`` — the same fallback the
    CLI makes (config.go:515 returns cwd), so the two agree on the answer even
    when the answer is a guess.
    """
    start = Path(config_path).expanduser().resolve().parent
    for candidate in (start, *start.parents):
        if (candidate / ROOT_MARKER).is_dir():
            return candidate, True
    return start, False


def preflight(
    config_path: str | Path, env: str, *, check_inventory: bool = True
) -> Preflight:
    """Predict the CLI's path resolution and report what is missing.

    Warnings are advisory on purpose. Refusing to run would break commands that
    work today — ``/instances`` reads the cloud API and needs no inventory at
    all. The point is that the operator learns about a missing file when they
    ask, instead of inferring it from a wall of identical host failures later.

    ``check_inventory`` should be False for commands that never reach into a
    host, so the warning does not fire on every cloud-only read. The caller
    decides, because which commands need an inventory is a property of the CLI's
    verbs (provision.go, lab_reset.go, runcmd.go, up.go, and health via
    config/provider.go) rather than of a path.
    """
    root, marker_found = resolve_root(config_path)
    inventory = root / f"{env}-inventory"
    warnings: list[str] = []

    if not marker_found:
        warnings.append(
            f"No {ROOT_MARKER}/ directory at or above {root}, so the CLI will "
            f"treat {root} as the project root by fallback. Inventory, lab data "
            f"and ansible config will all be resolved from there. Set "
            f"project_root in the config to make this explicit."
        )

    if check_inventory and not inventory.exists():
        # Name a sibling if one exists: the usual cause is a config pointed at
        # the wrong environment, or an inventory that was never generated, and
        # the two look identical from the error alone.
        siblings = sorted(p.name for p in root.glob("*-inventory") if p.is_file())
        detail = f"Found instead: {', '.join(siblings)}." if siblings else ""
        warnings.append(
            f"No inventory at {inventory}. Commands that reach into hosts "
            f"(/health, /provision, /reset, /exec) will fail for every host "
            f"until it exists. {detail}".strip()
        )

    return Preflight(
        root=root, marker_found=marker_found, inventory=inventory, warnings=warnings
    )


def config_path_of(session: dict[str, t.Any]) -> str | None:
    """The config path recorded on a session's anchor, if it has one."""
    anchor = session.get("anchor") or {}
    path = anchor.get("config_path")
    return str(path) if path else None


def run_cwd(session: dict[str, t.Any], default: str | Path) -> str:
    """Working directory for any CLI spawn on behalf of ``session``.

    Every spawn must agree on this. The console has four — the operator/agent
    command pipeline, file fetches, and the inventory and topology syncs — and
    a subset running in a different tree would resolve a different inventory
    and lab data than the commands whose results they are recording.

    ``default`` is used when the session has no anchor to derive from; it is
    the console's own repo root, which is where the binary lives and the only
    sensible guess left.
    """
    config_path = config_path_of(session)
    if not config_path:
        return str(default)
    root, _ = resolve_root(config_path)
    return str(root)
