"""Fetch an agent's report from the attack box for /score (design §5.2).

The report is written on the Kali attack box; ``dreadgoad score --report`` takes
a **local** path, so ``/score`` pulls the file first:

  - **AWS**: scp over an SSM SSH proxy (no inbound ports; matches the
    no-open-ports design).
  - **Azure**: scp using the discovered SSH key (over the bastion).

Requires ``snapshot.attack_box`` (and, for Azure, ``ssh_key``) — both discovered
post-deploy. ``build_fetch_argv`` (the transfer command) is unit-tested; the
live transfer needs cloud + those credentials and is verified manually.
"""

from __future__ import annotations

import os
import typing as t

from .cli import capture


def build_fetch_argv(
    snapshot: dict[str, t.Any], remote_path: str, local_path: str
) -> list[str]:
    """Construct an scp command to pull ``remote_path`` → ``local_path``.

    Raises ValueError if the attack box (or Azure SSH key) isn't known yet.
    """
    provider = snapshot.get("provider")
    box = snapshot.get("attack_box")
    if not box:
        raise ValueError(
            "attack box not known yet (discovered post-deploy); cannot fetch report"
        )

    if provider == "aws":
        region = snapshot.get("region") or ""
        proxy = (
            "aws ssm start-session --target %h "
            "--document-name AWS-StartSSHSession --parameters portNumber=%p"
        )
        if region:
            proxy += f" --region {region}"
        return [
            "scp",
            "-o",
            "StrictHostKeyChecking=no",
            "-o",
            f"ProxyCommand={proxy}",
            f"kali@{box}:{remote_path}",
            local_path,
        ]

    if provider == "azure":
        az = snapshot.get("azure") or {}
        ssh_key = az.get("ssh_key")
        ssh_user = az.get("ssh_user") or "kali"
        if not ssh_key:
            raise ValueError("azure ssh_key not known yet; cannot fetch report")
        return [
            "scp",
            "-i",
            str(ssh_key),
            "-o",
            "StrictHostKeyChecking=no",
            f"{ssh_user}@{box}:{remote_path}",
            local_path,
        ]

    raise ValueError(f"unsupported provider for report fetch: {provider!r}")


def local_report_path(session_dir: str, remote_path: str) -> str:
    """Where the fetched report lands inside the session working dir."""
    name = os.path.basename(remote_path) or "report.jsonl"
    return os.path.join(session_dir, name)


async def fetch_report(
    snapshot: dict[str, t.Any], session_dir: str, remote_path: str
) -> tuple[int, str, str]:
    """Fetch the report into the session dir. Returns (rc, local_path, message)."""
    local = local_report_path(session_dir, remote_path)
    argv = build_fetch_argv(snapshot, remote_path, local)
    rc, out, err = await capture(argv, cwd=".")
    return rc, local, (err or out)
