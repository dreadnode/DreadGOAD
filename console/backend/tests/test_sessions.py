"""Tests for SessionService (Phase 2: T2.2).

Standalone:  python console/backend/tests/test_sessions.py
"""

from __future__ import annotations

import asyncio
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import configstore, paths  # noqa: E402
from console.backend.db import Database  # noqa: E402
from console.backend.sessions import SessionService, default_label  # noqa: E402

_REPO = pathlib.Path(__file__).resolve().parents[3]

_YAML = """\
provider: azure
region: centralus
environments:
  staging:
    variant: true
    variant_source: ad/GOAD
    variant_target: ad/GOAD
    variant_name: dreadindex
    vpc_cidr: "10.1.0.0/16"
"""


def _project_root(tmp: pathlib.Path) -> None:
    """Make ``tmp`` a tree the lab lookup will accept.

    The lab config is resolved from the *config's own* project root, matching
    where the CLI will look for it (projectroot.resolve_root). A config dropped
    in a bare temp directory therefore has no lab, which is correct — the CLI
    would not find one either. These tests want a config that does, so give the
    directory the ``ansible/`` marker and the repo's ``ad/`` tree.
    """
    (tmp / "ansible").mkdir(exist_ok=True)
    link = tmp / "ad"
    if not link.exists():
        # Symlinked rather than copied: ad/ is ~19MB and read-only here.
        link.symlink_to(_REPO / "ad")


async def _svc(tmp: pathlib.Path) -> SessionService:
    _project_root(tmp)
    db = await Database(str(tmp / "state.db")).connect()
    return SessionService(db, repo_root=str(_REPO), sessions_root=tmp / "sessions")


async def test_create_attach_session() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            s = await svc.create_session(str(cfg), "staging", model="m")
            sid = s["id"]

            # session persisted with anchor + snapshot
            got = await svc.get_session(sid)
            assert got is not None and got["anchor"]["env"] == "staging", got
            assert got["snapshot"]["provider"] == "azure", got

            # working dir created
            assert os.path.isdir(got["session_dir"]), "session dir not created"

            # range seeded from ad/GOAD/data/config.json (variant_target=ad/GOAD)
            rng = await svc.db.get_range(sid)
            assert rng is not None, "range row missing"
            ids = {h["id"] for h in rng["hosts"]}
            assert "kingslanding" in ids, ids
            assert "attackbox" in ids and "bastion" in ids, "infra nodes missing"

            # session_created event recorded
            evts = await svc.db.get_events(sid)
            assert any(e["kind"] == "session_created" for e in evts), evts

            assert len(await svc.list_sessions()) == 1
            print("PASS test_create_attach_session")
        finally:
            await svc.db.close()


async def test_delete_session_removes_dir_and_rows() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            s = await svc.create_session(str(cfg), "staging")
            sdir = s["session_dir"]
            assert os.path.isdir(sdir)
            ok = await svc.delete_session(s["id"])
            assert ok, "delete returned False"
            assert await svc.get_session(s["id"]) is None, "session row remains"
            assert await svc.db.get_range(s["id"]) is None, "range row remains"
            assert not os.path.isdir(sdir), "session dir not removed"
            print("PASS test_delete_session_removes_dir_and_rows")
        finally:
            await svc.db.close()


async def test_delete_refuses_working_dir_outside_session_root() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            session = await svc.create_session(str(cfg), "staging")
            outside = tmp / "must-not-delete"
            outside.mkdir()
            (outside / "sentinel").write_text("keep")
            session["session_dir"] = str(outside)
            await svc.db.upsert_session(session)

            try:
                await svc.delete_session(session["id"])
            except ValueError:
                pass
            else:
                raise AssertionError("unsafe session directory was accepted")

            assert outside.exists() and (outside / "sentinel").exists()
            assert await svc.get_session(session["id"]) is not None
            print("PASS test_delete_refuses_working_dir_outside_session_root")
        finally:
            await svc.db.close()


async def test_create_new_env_writes_yaml_and_backs_up() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            s = await svc.create_new_env_session(
                str(cfg),
                "prod",
                env_fields={"variant_source": "ad/GOAD", "vpc_cidr": "10.9.0.0/16"},
                label="prod range",
            )
            # backup written
            assert (tmp / "dreadgoad.yaml.bak.1").is_file(), "no backup created"
            # new env present in the yaml
            import yaml

            data = yaml.safe_load(cfg.read_text())
            assert "prod" in data["environments"], data["environments"].keys()
            # session anchored to the new env
            assert s["anchor"]["env"] == "prod", s
            print("PASS test_create_new_env_writes_yaml_and_backs_up")
        finally:
            await svc.db.close()


_YAML_GREENFIELD = """\
provider: aws
region: us-west-2
environments:
  dev:
    variant_source: ad/DOES-NOT-EXIST
    variant_target: ad/DOES-NOT-EXIST
    vpc_cidr: "10.0.0.0/16"
"""


async def test_greenfield_seeds_infra_only() -> None:
    """A range whose lab dir doesn't exist yet seeds infra nodes only (§6.3)."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML_GREENFIELD)
        svc = await _svc(tmp)
        try:
            s = await svc.create_session(str(cfg), "dev")
            rng = await svc.db.get_range(s["id"])
            assert rng is not None
            ids = {h["id"] for h in rng["hosts"]}
            # No config hosts (lab dir missing); aws → attackbox only, no bastion.
            assert ids == {"attackbox"}, f"greenfield should seed infra-only, got {ids}"
            print("PASS test_greenfield_seeds_infra_only")
        finally:
            await svc.db.close()


def test_default_label_names_the_config_and_env() -> None:
    """The tab answers 'which session'; the range header answers 'what is it'."""
    cases = [
        # Ordinary case: variant name matches the env, so it adds nothing.
        (
            "/repo/dreadgoad.yaml",
            "staging",
            {"variant_name": "staging"},
            "dreadgoad/staging",
        ),
        # A console-created config keeps its own name, which is the whole point
        # once more than one config can exist.
        (
            "/r/.dreadgoad/console/configs/azure-lab.yaml",
            "redteam",
            {"variant_name": "redteam"},
            "azure-lab/redteam",
        ),
        # Variant deliberately different from the env — the one case where the
        # third segment carries information.
        (
            "/repo/dreadgoad.yaml",
            "redteam",
            {"variant_name": "phase2"},
            "dreadgoad/redteam · phase2",
        ),
        # No variant at all.
        ("/repo/dreadgoad.yaml", "prod", {}, "dreadgoad/prod"),
        # The provider is deliberately NOT in the label: the range header
        # already shows it, so repeating it spends tab width on nothing.
        ("/repo/dreadgoad.yaml", "prod", {"provider": "azure"}, "dreadgoad/prod"),
    ]
    for config_path, env, snap, expected in cases:
        got = default_label(config_path, env, snap)
        assert got == expected, (config_path, env, got, expected)
    print("PASS test_default_label_names_the_config_and_env")


async def test_create_config_session_writes_a_config_and_attaches() -> None:
    """One call makes the config, the env inside it, and the session."""
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        try:
            s = await svc.create_config_session(
                "Azure Lab #1",
                "azure",
                "redteam",
                {
                    "variant": True,
                    "variant_source": "ad/GOAD",
                    "vpc_cidr": "10.7.0.0/16",
                },
                region="eastus",
            )
            path = pathlib.Path(s["anchor"]["config_path"])
            assert path.name == "azure-lab-1.yaml", f"name not slugged: {path}"
            assert path.parent == paths.configs_root().resolve(), path

            # The session reads back through the same snapshot path as any other.
            assert s["anchor"]["env"] == "redteam", s
            assert s["snapshot"]["provider"] == "azure", s
            assert s["snapshot"]["region"] == "eastus", s

            # And the new config is discoverable by the picker straight away.
            listed = {c["path"]: c for c in configstore.known_configs()}
            assert str(path) in listed, listed.keys()
            assert listed[str(path)]["source"] == "managed", listed[str(path)]
            assert listed[str(path)]["environments"] == ["redteam"], listed[str(path)]
            print("PASS test_create_config_session_writes_a_config_and_attaches")
        finally:
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved


async def test_create_config_session_refuses_a_taken_name() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        try:
            await svc.create_config_session("lab", "aws", "one", {}, region="us-east-1")
            try:
                # Same name, different provider: the dangerous case. Silently
                # merging would hand the second session the first's provider.
                await svc.create_config_session("lab", "azure", "two", {})
                raise AssertionError("expected FileExistsError for a taken name")
            except FileExistsError:
                pass
            # The first config is intact and still the only one.
            path = paths.configs_root().resolve() / "lab.yaml"
            import yaml

            data = yaml.safe_load(path.read_text())
            assert data["provider"] == "aws", data
            assert list(data["environments"]) == ["one"], data
            assert len(await svc.list_sessions()) == 1, "no session for a failed create"
            print("PASS test_create_config_session_refuses_a_taken_name")
        finally:
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved


async def test_create_config_session_rolls_back_on_failure() -> None:
    """A half-made config would block retrying under the same name."""
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        try:
            boom = RuntimeError("db is down")

            async def fail(*_a: object, **_k: object) -> dict[str, object]:
                raise boom

            original = svc.create_session
            svc.create_session = fail  # type: ignore[method-assign]
            try:
                await svc.create_config_session("doomed", "aws", "dev", {})
                raise AssertionError("expected the injected failure to propagate")
            except RuntimeError as exc:
                assert exc is boom, exc
            finally:
                svc.create_session = original  # type: ignore[method-assign]

            assert not (paths.configs_root().resolve() / "doomed.yaml").exists(), (
                "a failed create must not leave a config that blocks the retry"
            )
            # Proven by the retry actually succeeding.
            s = await svc.create_config_session("doomed", "aws", "dev", {})
            assert s["anchor"]["env"] == "dev", s
            print("PASS test_create_config_session_rolls_back_on_failure")
        finally:
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved


async def _main() -> None:
    await test_create_attach_session()
    await test_delete_session_removes_dir_and_rows()
    await test_delete_refuses_working_dir_outside_session_root()
    await test_create_new_env_writes_yaml_and_backs_up()
    await test_greenfield_seeds_infra_only()
    test_default_label_names_the_config_and_env()
    await test_create_config_session_writes_a_config_and_attaches()
    await test_create_config_session_refuses_a_taken_name()
    await test_create_config_session_rolls_back_on_failure()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
