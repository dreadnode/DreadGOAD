"""Fetch an agent's report from the attack box for /score (design §5.2).

The report is written on the Kali attack box; ``dreadgoad score --report`` takes
a **local** path, so ``/score`` pulls the file first. Rather than hand-rolling
scp/SSM/Bastion in Python, this drives ``dreadgoad score fetch``, which reuses
the CLI's own connection machinery:

  - **AWS**: SSM (no inbound ports). Pass the Kali instance id (learned by the
    inventory sync post-deploy, see ``inventory_sync.find_attack_box``).
  - **Azure**: over Azure Bastion. The CLI auto-discovers the Kali VM and its
    SSH key, so nothing extra is needed.

``build_fetch_argv`` (the command) is unit-tested; the live transfer needs cloud
and is verified manually.
"""

from __future__ import annotations

import os
import typing as t

from . import commands, paths, projectroot
from .cli import capture

Capture = t.Callable[[list[str], str], t.Awaitable[tuple[int, str, str]]]


def build_fetch_argv(
    session: dict[str, t.Any],
    remote_path: str,
    local_path: str,
    repo_root: str = ".",
) -> list[str]:
    """Construct a ``dreadgoad score fetch`` argv for the session's range.

    Shape: ``[bin, --config, --env, score, fetch, --remote, --local, <provider…>]``.
    Raises ValueError if the provider is unsupported, or (AWS) the attack box
    isn't known yet.
    """
    anchor = session["anchor"]
    snap = session.get("snapshot") or {}
    provider = snap.get("provider")

    argv = [
        commands.resolve_bin(repo_root),
        "--config",
        str(anchor["config_path"]),
        "--env",
        str(anchor["env"]),
        "score",
        "fetch",
        "--remote",
        remote_path,
        "--local",
        local_path,
    ]

    if provider == "aws":
        box = snap.get("attack_box")
        if not box:
            raise ValueError(
                "attack box not known yet (discovered post-deploy); "
                "run a command like /instances first"
            )
        argv += ["--attack-box", str(box)]
        region = snap.get("region")
        if region:
            argv += ["--region", str(region)]
    elif provider == "azure":
        # The CLI auto-discovers the Kali VM + SSH key over Bastion. Don't pass
        # --attack-box (an explicit Azure resource id requires --ssh-key and
        # skips key auto-discovery); only forward a key if the snapshot has one.
        ssh_key = (snap.get("azure") or {}).get("ssh_key")
        if ssh_key:
            argv += ["--ssh-key", str(ssh_key)]
    else:
        raise ValueError(f"unsupported provider for report fetch: {provider!r}")

    return argv


def local_report_path(session_dir: str, remote_path: str) -> str:
    """Where the fetched report lands inside the session working dir.

    Only the remote basename is kept, so a path like ``../../etc/shadow`` lands
    as ``shadow`` in the session dir. ``.``/``..`` survive basename() and would
    name the session dir itself or its parent, so they fall back to the default
    too — the destination must always be a *file* inside the session dir.
    """
    name = os.path.basename(remote_path)
    if name in ("", ".", ".."):
        name = "report.jsonl"
    return os.path.join(session_dir, name)


async def fetch_report(
    session: dict[str, t.Any],
    remote_path: str,
    capture_command: Capture | None = None,
) -> tuple[int, str, str]:
    """Fetch the report into the session dir. Returns (rc, local_path, message)."""
    local = local_report_path(session["session_dir"], remote_path)
    # repo_root locates the binary; the cwd decides which tree's inventory the
    # fetch reaches hosts through. They are different questions — see
    # projectroot.run_cwd.
    argv = build_fetch_argv(session, remote_path, local, str(paths.repo_root()))
    runner = capture_command or capture
    rc, out, err = await runner(argv, projectroot.run_cwd(session, paths.repo_root()))
    return rc, local, (err or out)
