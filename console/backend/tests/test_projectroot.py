"""Tests for project-root resolution and the pre-spawn preflight.

Built on real directory trees rather than patched path functions: the module
exists to predict what the Go CLI will find on disk, and a mocked filesystem
would let a wrong prediction pass.

Standalone:  python console/backend/tests/test_projectroot.py
"""

from __future__ import annotations

import asyncio
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend.projectroot import (  # noqa: E402
    ROOT_MARKER,
    config_path_of,
    preflight,
    resolve_root,
)
from console.backend.projectroot import run_cwd as projectroot_run_cwd  # noqa: E402

_YAML = "provider: azure\nenvironments:\n  dreadindex2: {}\n"


def _tree(
    base: pathlib.Path, name: str, *, marker: bool, inventories=()
) -> pathlib.Path:
    """A checkout: a config, optionally an ansible/ marker, and inventories.

    Returns a fully resolved path. On macOS the temp dir is /var/..., itself a
    symlink to /private/var/..., and resolve_root resolves the config before
    walking — deliberately, so a symlinked config lands in the tree the file
    really lives in. Comparing against an unresolved fixture path would fail on
    that symlink rather than on any behaviour under test.
    """
    root = (base / name).resolve()
    root.mkdir(parents=True)
    if marker:
        (root / ROOT_MARKER).mkdir()
    (root / "dreadgoad-dreadindex2.yaml").write_text(_YAML)
    for inv in inventories:
        (root / inv).write_text("[dc]\nDC01\n")
    return root


def test_the_reported_layout() -> None:
    """Two checkouts, config in one, console running from the other.

    The real case: ~/dev/DreadGOAD holds the config and dreadindex2-inventory;
    the console ran the CLI in a worktree that had ansible/ but only an
    unrelated dreadindex-inventory. Resolution must follow the config.
    """
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        real = _tree(
            base, "DreadGOAD", marker=True, inventories=("dreadindex2-inventory",)
        )
        worktree = _tree(
            base, "worktree", marker=True, inventories=("dreadindex-inventory",)
        )

        checks = preflight(real / "dreadgoad-dreadindex2.yaml", "dreadindex2")
        assert checks.root == real, checks.root
        assert checks.root != worktree
        assert checks.inventory == real / "dreadindex2-inventory"
        assert checks.marker_found is True
        # Everything is present, so nothing to warn about.
        assert checks.warnings == [], checks.warnings
        print("PASS test_the_reported_layout")


def test_missing_inventory_is_warned_with_the_path_and_siblings() -> None:
    """What the operator should have been told immediately.

    The old failure mode was 22 identical host errors, twenty-five minutes in.
    """
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        # The worktree's own layout: marker present, wrong inventory.
        root = _tree(
            base, "worktree", marker=True, inventories=("dreadindex-inventory",)
        )

        checks = preflight(root / "dreadgoad-dreadindex2.yaml", "dreadindex2")
        assert len(checks.warnings) == 1, checks.warnings
        warning = checks.warnings[0]
        assert str(root / "dreadindex2-inventory") in warning, warning
        assert "/health" in warning, warning
        # The near-miss sibling is named: that is the whole diagnosis.
        assert "dreadindex-inventory" in warning, warning
        print("PASS test_missing_inventory_is_warned_with_the_path_and_siblings")


def test_cloud_only_reads_do_not_warn_about_inventory() -> None:
    """/instances needs no inventory, so it must not be warned about one.

    The preflight runs before every command. Warning on reads that cannot use
    an inventory trains the operator to ignore the line that matters — and
    /instances is the command the console runs most.
    """
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        root = _tree(base, "worktree", marker=True)  # no inventory at all
        cfg = root / "dreadgoad-dreadindex2.yaml"

        host_cmd = preflight(cfg, "dreadindex2", check_inventory=True)
        cloud_cmd = preflight(cfg, "dreadindex2", check_inventory=False)

        assert len(host_cmd.warnings) == 1, host_cmd.warnings
        assert cloud_cmd.warnings == [], cloud_cmd.warnings
        # The root is identical either way; only the warning is suppressed.
        assert cloud_cmd.root == host_cmd.root
        assert cloud_cmd.inventory == host_cmd.inventory
        print("PASS test_cloud_only_reads_do_not_warn_about_inventory")


def test_the_gate_matches_the_registry() -> None:
    """The commands gated in are exactly the ones that drive hosts.

    Pins the mapping from evidence rather than memory: if a future edit makes
    /instances long_running, or /health not, this fails instead of silently
    changing who gets warned.
    """
    from console.backend import commands

    needs_hosts = {"/health", "/provision", "/reset", "/exec", "/up", "/validate"}
    gated = {n for n, c in commands.REGISTRY.items() if c.long_running}

    assert needs_hosts <= gated, f"not gated in: {needs_hosts - gated}"
    assert not commands.REGISTRY["/instances"].long_running
    print(f"PASS test_the_gate_matches_the_registry ({len(gated)} gated)")


def test_config_in_a_subdirectory_walks_up() -> None:
    # Configs are often kept in a subdir; the CLI walks up, so this must too.
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        root = _tree(
            base, "DreadGOAD", marker=True, inventories=("dreadindex2-inventory",)
        )
        nested = root / "configs" / "azure"
        nested.mkdir(parents=True)
        cfg = nested / "dreadgoad-dreadindex2.yaml"
        cfg.write_text(_YAML)

        found, marker = resolve_root(cfg)
        assert found == root, found
        assert marker is True
        assert preflight(cfg, "dreadindex2").warnings == []
        print("PASS test_config_in_a_subdirectory_walks_up")


def test_no_marker_anywhere_falls_back_and_says_so() -> None:
    """The case that makes cwd-from-config a guess rather than a fix.

    With no ansible/ above the config the CLI falls back to its working
    directory, so the console lands on the same answer — but the operator is
    told it was a fallback, because every derived path is then suspect.
    """
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        loose = base / "configs"
        loose.mkdir()
        cfg = loose / "dreadgoad-dreadindex2.yaml"
        cfg.write_text(_YAML)

        root, marker = resolve_root(cfg)
        assert root == loose.resolve(), root
        assert marker is False

        checks = preflight(cfg, "dreadindex2")
        assert len(checks.warnings) == 2, checks.warnings
        assert "project_root" in checks.warnings[0], checks.warnings[0]
        assert ROOT_MARKER in checks.warnings[0], checks.warnings[0]
        print("PASS test_no_marker_anywhere_falls_back_and_says_so")


def test_nearest_marker_wins() -> None:
    # Nested checkouts: the walk must stop at the first marker going up, which
    # is what the CLI does.
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        outer = _tree(base, "outer", marker=True)
        inner = _tree(
            outer, "inner", marker=True, inventories=("dreadindex2-inventory",)
        )

        found, _ = resolve_root(inner / "dreadgoad-dreadindex2.yaml")
        assert found == inner, found
        print("PASS test_nearest_marker_wins")


def test_symlinked_config_resolves_to_the_real_tree() -> None:
    # A config symlinked into another tree must resolve where the file really
    # lives, not where the link sits, or the walk starts in the wrong place.
    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        real = _tree(
            base, "DreadGOAD", marker=True, inventories=("dreadindex2-inventory",)
        )
        elsewhere = base / "links"
        elsewhere.mkdir()
        link = elsewhere / "cfg.yaml"
        link.symlink_to(real / "dreadgoad-dreadindex2.yaml")

        found, marker = resolve_root(link)
        assert found == real, found
        assert marker is True
        print("PASS test_symlinked_config_resolves_to_the_real_tree")


def test_config_path_of() -> None:
    assert config_path_of({"anchor": {"config_path": "/x/y.yaml"}}) == "/x/y.yaml"
    # A session without an anchor must not raise — the caller falls back.
    assert config_path_of({}) is None
    assert config_path_of({"anchor": {}}) is None
    assert config_path_of({"anchor": {"config_path": ""}}) is None
    print("PASS test_config_path_of")


def test_run_cwd_falls_back_without_an_anchor() -> None:
    assert projectroot_run_cwd({}, "/fallback") == "/fallback"
    assert projectroot_run_cwd({"anchor": {}}, "/fallback") == "/fallback"
    print("PASS test_run_cwd_falls_back_without_an_anchor")


async def test_every_spawn_site_uses_the_same_tree() -> None:
    """All four CLI spawns must run in the config's tree, not the console's.

    The first version of this fix changed only the command pipeline and left
    fetch, inventory sync and topology sync on the console's repo root — so a
    fetch would reach hosts through one tree's inventory while the commands it
    followed used another. Asserted by capturing the cwd each one passes.
    """
    from console.backend import fetch, paths, topology_sync

    with tempfile.TemporaryDirectory() as d:
        base = pathlib.Path(d)
        real = _tree(
            base, "DreadGOAD", marker=True, inventories=("dreadindex2-inventory",)
        )
        cfg = real / "dreadgoad-dreadindex2.yaml"
        session = {
            "id": "s-1",
            "anchor": {"config_path": str(cfg), "env": "dreadindex2"},
            "snapshot": {"provider": "azure", "lab": None},
            "session_dir": str(base / "sess"),
        }
        (base / "sess").mkdir()

        seen: list[str] = []

        async def spy(argv: list[str], cwd: str) -> tuple[int, str, str]:
            seen.append(cwd)
            return 0, "[]", ""

        await fetch.fetch_report(session, "/remote/report.jsonl", spy)
        await topology_sync.extension_nodes(session, spy)

        console_root = str(paths.repo_root())
        assert seen, "no spawn was captured"
        for cwd in seen:
            assert cwd == str(real), f"spawned in {cwd}, expected {real}"
            assert cwd != console_root
        print(f"PASS test_every_spawn_site_uses_the_same_tree ({len(seen)} spawns)")


def test_no_spawn_site_passes_the_console_repo_as_cwd() -> None:
    """No CLI spawn may use repo_root() as its working directory.

    The live test above drives two of the four spawn sites; inventory sync
    needs a database fixture to reach. This covers all four structurally, and
    is the check that would have caught the three I missed on the first pass —
    the failure mode is a *forgotten* call site, which a test that only
    exercises the ones you remembered cannot find.

    repo_root() is still legitimate for locating the binary and the console's
    own state, so this looks only at what is passed as a cwd.
    """
    backend = pathlib.Path(__file__).resolve().parents[1]
    spawners = ("runner(", "start_command(", "capture(")
    offenders: list[str] = []

    for name in (
        "command_runner.py",
        "fetch.py",
        "inventory_sync.py",
        "topology_sync.py",
    ):
        text = (backend / name).read_text()
        # Join wrapped calls so a multi-line spawn is inspected as one unit.
        flat = " ".join(text.split())
        for spawner in spawners:
            start = 0
            while (idx := flat.find(spawner, start)) != -1:
                call = flat[idx : idx + 160]
                start = idx + 1
                if "argv" not in call:
                    continue
                if "paths.repo_root()" in call and "run_cwd" not in call:
                    offenders.append(f"{name}: {call[:100]}")

    assert not offenders, (
        "spawn(s) still using the console's repo as cwd:\n" + "\n".join(offenders)
    )
    # And every module that spawns must reach the helper at all.
    for name in ("fetch.py", "inventory_sync.py", "topology_sync.py"):
        assert "projectroot.run_cwd" in (backend / name).read_text(), name
    assert "run_cwd = " in (backend / "command_runner.py").read_text()
    print("PASS test_no_spawn_site_passes_the_console_repo_as_cwd")


def main() -> None:
    test_the_reported_layout()
    test_missing_inventory_is_warned_with_the_path_and_siblings()
    test_cloud_only_reads_do_not_warn_about_inventory()
    test_the_gate_matches_the_registry()
    test_config_in_a_subdirectory_walks_up()
    test_no_marker_anywhere_falls_back_and_says_so()
    test_nearest_marker_wins()
    test_symlinked_config_resolves_to_the_real_tree()
    test_config_path_of()
    test_run_cwd_falls_back_without_an_anchor()
    test_no_spawn_site_passes_the_console_repo_as_cwd()
    asyncio.run(test_every_spawn_site_uses_the_same_tree())
    print("ALL PASS")


if __name__ == "__main__":
    main()
