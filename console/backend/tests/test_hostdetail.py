"""Tests for per-host attached-resource detail (disks and NICs).

Standalone:  python console/backend/tests/test_hostdetail.py
"""

from __future__ import annotations

import asyncio
import json
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import hostdetail  # noqa: E402

_SESSION = {
    "anchor": {"config_path": "/repo/dreadgoad.yaml", "env": "rt"},
    "snapshot": {"provider": "azure"},
}
_ARM_ID = (
    "/subscriptions/70a9/resourceGroups/DG-RG/providers/"
    "Microsoft.Compute/virtualMachines/dg-goad-DC01-vm"
)
_RANGE = {
    "hosts": [
        # A variant range: the node id is the randomised hostname, the key is
        # the config role, and only cloud_id names the VM in Azure.
        {"id": "nova", "key": "dc01", "hostname": "nova", "cloud_id": _ARM_ID},
        {"id": "cascade", "key": "dc03", "hostname": "cascade", "cloud_id": None},
    ]
}


def _capture(payload: str, rc: int = 0, err: str = ""):
    seen: list[tuple[list[str], str]] = []

    async def run(argv: list[str], cwd: str) -> tuple[int, str, str]:
        seen.append((argv, cwd))
        return rc, payload, err

    return run, seen


def test_find_host_matches_on_node_id() -> None:
    assert hostdetail.find_host(_RANGE, "nova")["key"] == "dc01"
    assert hostdetail.find_host(_RANGE, "dc01") is None, (
        "lookup is by node id; the key is what Azure names contain, not what "
        "the graph addresses a node by"
    )
    assert hostdetail.find_host({"hosts": []}, "nova") is None
    print("PASS test_find_host_matches_on_node_id")


def test_build_argv_passes_the_resource_id() -> None:
    argv = hostdetail.build_argv("/c.yaml", "rt", _ARM_ID)
    assert argv[1:] == [
        "--config",
        "/c.yaml",
        "--env",
        "rt",
        "lab",
        "describe",
        "--id",
        _ARM_ID,
        "--json",
    ], argv
    # Never the hostname: resolution would list every VM in the subscription
    # and substring-match on the config role, which is not the node id.
    assert "nova" not in argv, argv
    print("PASS test_build_argv_passes_the_resource_id")


async def test_detail_returns_the_cli_payload_tagged_with_its_node() -> None:
    payload = json.dumps(
        {
            "id": _ARM_ID,
            "name": "dg-goad-DC01-vm",
            "resource_group": "DG-RG",
            "disks": [{"name": "dc01-osdisk", "role": "os", "size_gb": 128}],
            "nics": [{"name": "dc01-nic", "private_ips": ["10.100.1.4"]}],
        }
    )
    run, seen = _capture(payload)
    got = await hostdetail.host_detail(_SESSION, _RANGE, "nova", capture_command=run)

    assert got["disks"][0]["name"] == "dc01-osdisk", got
    assert got["nics"][0]["private_ips"] == ["10.100.1.4"], got
    # Tagged so a response arriving after the operator clicked elsewhere can be
    # discarded rather than rendered against the wrong node.
    assert got["node_id"] == "nova", got

    argv, _cwd = seen[0]
    assert _ARM_ID in argv, argv
    print("PASS test_detail_returns_the_cli_payload_tagged_with_its_node")


async def test_undeployed_host_is_a_reason_not_a_crash() -> None:
    """cloud_id is empty until the ingestion hook has seen the VM.

    That is the normal state before a deploy — and after one, until /instances
    has run. The panel says so; it does not report an error.
    """
    run, seen = _capture("{}")
    try:
        await hostdetail.host_detail(_SESSION, _RANGE, "cascade", capture_command=run)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "not been deployed" in str(exc), exc
    assert seen == [], "must not spawn the CLI for a host with no cloud id"
    print("PASS test_undeployed_host_is_a_reason_not_a_crash")


async def test_non_azure_provider_is_refused_not_emptied() -> None:
    """An empty panel would read as 'this VM has no disks'."""
    session = {**_SESSION, "snapshot": {"provider": "aws"}}
    run, seen = _capture("{}")
    try:
        await hostdetail.host_detail(session, _RANGE, "nova", capture_command=run)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "aws" in str(exc), exc
    assert seen == [], "must not spawn the CLI for an unsupported provider"
    print("PASS test_non_azure_provider_is_refused_not_emptied")


async def test_unknown_node_is_refused() -> None:
    run, _ = _capture("{}")
    try:
        await hostdetail.host_detail(_SESSION, _RANGE, "ghost", capture_command=run)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "ghost" in str(exc), exc
    print("PASS test_unknown_node_is_refused")


async def test_cli_failures_surface_as_reasons() -> None:
    cases = [
        (_capture("", rc=1, err="ResourceNotFound"), "ResourceNotFound"),
        (_capture("not json", rc=0), "unreadable JSON"),
        (_capture('["a list"]', rc=0), "unexpected shape"),
    ]
    for (run, _seen), expected in cases:
        try:
            await hostdetail.host_detail(_SESSION, _RANGE, "nova", capture_command=run)
            raise AssertionError(f"expected failure for {expected}")
        except hostdetail.HostDetailUnavailable as exc:
            assert expected in str(exc), (expected, str(exc))

    # A missing binary raises from create_subprocess_exec rather than returning
    # non-zero — the same shape that made /api/labs 500 once already.
    async def boom(_argv: list[str], _cwd: str) -> tuple[int, str, str]:
        raise FileNotFoundError(2, "No such file or directory")

    try:
        await hostdetail.host_detail(_SESSION, _RANGE, "nova", capture_command=boom)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "could not run" in str(exc), exc
    print("PASS test_cli_failures_surface_as_reasons")


async def test_malformed_documents_are_reasons_not_500s() -> None:
    """The endpoint's contract is a reason, never a stack trace.

    Both documents are written by this backend, so these shapes are defensive —
    but an older session row reaching them turned a well-formed request into a
    500, which the panel renders as an error rather than an explanation.
    """
    run, seen = _capture("{}")

    # A host entry that is not a mapping raised AttributeError out of find_host.
    try:
        await hostdetail.host_detail(
            _SESSION, {"hosts": ["not-a-dict"]}, "nova", capture_command=run
        )
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "no host" in str(exc), exc

    # An anchor without an env raised KeyError.
    anchored = {"anchor": {"config_path": "/c.yaml"}, "snapshot": {"provider": "azure"}}
    try:
        await hostdetail.host_detail(anchored, _RANGE, "nova", capture_command=run)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "no lab config anchored" in str(exc), exc

    # A non-string cloud_id was str()'d into a nonsense --id and spent a call.
    junk = {"hosts": [{"id": "n", "hostname": "n", "cloud_id": {"x": 1}}]}
    try:
        await hostdetail.host_detail(_SESSION, junk, "n", capture_command=run)
        raise AssertionError("expected HostDetailUnavailable")
    except hostdetail.HostDetailUnavailable as exc:
        assert "not been deployed" in str(exc), exc

    assert seen == [], "no malformed document should reach the CLI"
    print("PASS test_malformed_documents_are_reasons_not_500s")


async def _main() -> None:
    test_find_host_matches_on_node_id()
    test_build_argv_passes_the_resource_id()
    await test_detail_returns_the_cli_payload_tagged_with_its_node()
    await test_undeployed_host_is_a_reason_not_a_crash()
    await test_non_azure_provider_is_refused_not_emptied()
    await test_unknown_node_is_refused()
    await test_cli_failures_surface_as_reasons()
    await test_malformed_documents_are_reasons_not_500s()
    print("ALL PASS")


if __name__ == "__main__":
    os.environ.setdefault("DREADGOAD_CONSOLE_STATE_ROOT", tempfile.mkdtemp())
    asyncio.run(_main())
