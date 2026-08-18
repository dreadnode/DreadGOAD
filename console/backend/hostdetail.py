"""Attached-resource detail for one range host (disks and NICs).

Fetched on demand rather than folded into ``lab status --json``. That command
runs on every ``/instances`` and again after every command through the ingestion
hook, and a per-VM disk/NIC lookup would multiply its cloud calls to populate a
panel nobody has open. Nothing here is read unless an operator clicks a node.

Azure only: the CLI verb behind it type-asserts the Azure provider, the same
trade `bastion` makes. Other providers get a plain "not supported" rather than
an empty panel that looks like a VM with no disks.
"""

from __future__ import annotations

import json
import typing as t

from . import commands, paths, projectroot
from .cli import capture

Capture = t.Callable[[list[str], str], t.Awaitable[tuple[int, str, str]]]

SUPPORTED_PROVIDERS = ("azure",)


class HostDetailUnavailable(Exception):
    """Detail cannot be fetched, with an operator-facing reason."""


def find_host(rng: dict[str, t.Any], node_id: str) -> dict[str, t.Any] | None:
    """The range node with this id, or None.

    Skips entries that are not mappings rather than trusting the document's
    shape: this endpoint's contract is a reason, never a stack trace, and an
    AttributeError here would surface as a 500.
    """
    for host in rng.get("hosts") or []:
        if isinstance(host, dict) and host.get("id") == node_id:
            return host
    return None


def build_argv(config_path: str, env: str, cloud_id: str) -> list[str]:
    """The ``lab describe`` invocation for one VM.

    Passes --id rather than a hostname. Hostname resolution lists every VM in
    the subscription and substring-matches the name, and it matches on the
    config role (dc01), not the node's id, which for a variant range is a
    randomised hostname (nova) that appears nowhere in Azure. The node already
    carries the resource ID, so neither cost nor ambiguity is necessary.
    """
    return [
        commands.resolve_bin(str(paths.repo_root())),
        "--config",
        str(config_path),
        "--env",
        env,
        "lab",
        "describe",
        "--id",
        cloud_id,
        "--json",
    ]


def _clean_cli_error(raw: str) -> str:
    """Extract a human sentence from CLI stderr.

    The Azure SDK dumps a multi-line block that includes HTTP method, URL,
    status, headers, and a JSON body with ``error.message``.  Operators need
    the message, not the wire dump.
    """
    raw = raw.strip()
    msg = _extract_azure_message(raw)
    if msg:
        return msg
    if "RESPONSE 404" in raw or "ResourceNotFound" in raw:
        return "this VM no longer exists in Azure (404 ResourceNotFound)"
    return raw.split("\n", 1)[0][:200]


def _extract_azure_message(text: str) -> str | None:
    """Try to pull ``error.message`` from JSON in *text*.

    The JSON may be the whole string, or embedded between ``---`` separator
    lines in the Azure SDK's wire-dump format.
    """
    for candidate in _json_candidates(text):
        try:
            blob = json.loads(candidate)
            msg = blob.get("error", {}).get("message")
            if msg:
                return msg
        except (ValueError, AttributeError, TypeError):
            continue
    return None


def _json_candidates(text: str) -> t.Iterator[str]:
    """Yield substrings that might be JSON: the whole text, then every block
    of non-separator lines between ``---`` separator lines."""
    yield text
    block: list[str] = []
    for line in text.splitlines():
        if line.startswith("---"):
            if block:
                yield "\n".join(block)
                block = []
        else:
            block.append(line)
    if block:
        yield "\n".join(block)


async def host_detail(
    session: dict[str, t.Any],
    rng: dict[str, t.Any],
    node_id: str,
    capture_command: Capture | None = None,
) -> dict[str, t.Any]:
    """Disks and NICs for one node. Raises HostDetailUnavailable with a reason."""
    host = find_host(rng, node_id)
    if host is None:
        raise HostDetailUnavailable(f"no host {node_id!r} in this range")

    snapshot = session.get("snapshot") or {}
    provider = str(snapshot.get("provider") or "")
    if provider not in SUPPORTED_PROVIDERS:
        raise HostDetailUnavailable(
            f"attached-resource detail is not available for {provider or 'this provider'} yet"
        )

    cloud_id = host.get("cloud_id")
    if cloud_id is not None and not isinstance(cloud_id, str):
        # Anything else would be str()'d into a nonsense --id and spend a cloud
        # call proving it. Treat a malformed value as no value.
        cloud_id = None
    if not cloud_id:
        # The normal state before a deploy, and after one until /instances has
        # run: the node is seeded from the lab config and has no cloud identity
        # yet. Not an error worth a stack trace.
        raise HostDetailUnavailable(
            f"{host.get('hostname') or node_id} has not been deployed yet, "
            "or the range has not been read since it was"
        )

    # .get, not [...]: a session row written by an older schema (or a range
    # whose anchor never resolved) would raise KeyError here and turn a
    # well-formed request into a 500.
    anchor = session.get("anchor") or {}
    config_path = anchor.get("config_path")
    env = anchor.get("env")
    if not config_path or not env:
        raise HostDetailUnavailable(
            "this session has no lab config anchored to it, so its hosts cannot be read"
        )

    argv = build_argv(str(config_path), str(env), cloud_id)
    root, _ = projectroot.resolve_root(str(config_path))
    runner = capture_command or capture
    try:
        return_code, stdout, stderr = await runner(argv, str(root))
    except (OSError, ValueError) as exc:
        raise HostDetailUnavailable(
            f"could not run dreadgoad lab describe: {exc}"
        ) from exc

    if return_code != 0:
        raise HostDetailUnavailable(_clean_cli_error(stderr or stdout or "lab describe failed"))
    try:
        detail = json.loads(stdout)
    except ValueError as exc:
        raise HostDetailUnavailable(
            f"lab describe returned unreadable JSON: {exc}"
        ) from exc
    if not isinstance(detail, dict):
        raise HostDetailUnavailable("lab describe returned an unexpected shape")

    # Echo which node this belongs to; the panel is opened per node and a
    # response that cannot be tied back to one is a race waiting to render.
    detail["node_id"] = node_id
    return detail
