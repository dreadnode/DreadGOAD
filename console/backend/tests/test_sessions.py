"""Tests for SessionService (Phase 2: T2.2).

Standalone:  python console/backend/tests/test_sessions.py
"""

from __future__ import annotations

import asyncio
import copy
import os
import pathlib
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import configstore, paths, sessions as sessions_module  # noqa: E402
from console.backend.db import Database  # noqa: E402
from console.backend.sessions import SessionService, default_label  # noqa: E402

_REPO = pathlib.Path(__file__).resolve().parents[3]

_ENV = "session-unit"

_YAML = f"""\
provider: azure
region: centralus
environments:
  {_ENV}:
    lab: GOAD
    variant: false
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
            s = await svc.create_session(str(cfg), _ENV, model="m")
            sid = s["id"]

            # session persisted with anchor + snapshot
            got = await svc.get_session(sid)
            assert got is not None and got["anchor"]["env"] == _ENV, got
            assert got["snapshot"]["provider"] == "azure", got

            # working dir created
            assert os.path.isdir(got["session_dir"]), "session dir not created"
            assert (
                stat.S_IMODE(pathlib.Path(got["session_dir"]).stat().st_mode) == 0o700
            )
            assert stat.S_IMODE(svc.sessions_root.stat().st_mode) == 0o700

            # range seeded from the base GOAD config
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


async def test_service_repairs_existing_session_directory_modes() -> None:
    """Startup tightens real legacy session dirs without following symlinks."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        root = tmp / "sessions"
        legacy = root / "legacy-session"
        unrelated = tmp / "unrelated"
        legacy.mkdir(parents=True)
        unrelated.mkdir()
        legacy.chmod(0o755)
        unrelated.chmod(0o755)
        (root / "unexpected-link").symlink_to(unrelated)

        db = await Database(str(tmp / "state.db")).connect()
        try:
            svc = SessionService(db, repo_root=str(_REPO), sessions_root=root)
            assert stat.S_IMODE(svc.sessions_root.stat().st_mode) == 0o700
            assert stat.S_IMODE(legacy.stat().st_mode) == 0o700
            assert stat.S_IMODE(unrelated.stat().st_mode) == 0o755
        finally:
            await db.close()
    print("PASS test_service_repairs_existing_session_directory_modes")


async def test_delete_session_removes_dir_and_rows() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            s = await svc.create_session(str(cfg), _ENV)
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


async def test_create_session_records_initialization_results() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        original = sessions_module.lifecycle.initialize_session

        async def initialized(_session, _root):  # noqa: ANN001, ANN202
            return [
                {
                    "action": "generate_answer_key",
                    "status": "completed",
                    "message": "generated 12 scoring objectives",
                }
            ]

        sessions_module.lifecycle.initialize_session = initialized
        try:
            session = await svc.create_session(str(cfg), _ENV)
            events = await svc.db.get_events(session["id"])
            init_events = [
                event
                for event in events
                if (event.get("payload") or {}).get("initialization")
            ]
            assert len(init_events) == 1, init_events
            payload = init_events[0]["payload"]
            assert payload["initialization"]["status"] == "completed", payload
            assert "generated 12" in payload["content"], payload
        finally:
            sessions_module.lifecycle.initialize_session = original
            await svc.db.close()
    print("PASS test_create_session_records_initialization_results")


async def test_scaffold_retries_initialization_only_for_variants() -> None:
    """Only a variant is pending until its generated config is scaffolded."""
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        session = await svc.create_session(str(cfg), _ENV)
        initialized: list[str] = []
        original_scaffold = sessions_module.scaffold.scaffold_env
        original_initialize = svc._initialize

        async def scaffolded(*_args, **_kwargs):  # noqa: ANN002, ANN003, ANN202
            return True, "ok"

        async def initialize(current):  # noqa: ANN001, ANN202
            initialized.append(current["id"])

        sessions_module.scaffold.scaffold_env = scaffolded
        svc._initialize = initialize  # type: ignore[method-assign]
        try:
            await svc._scaffold_for(session, {"variant": False})
            assert initialized == [], initialized

            await svc._scaffold_for(session, {"variant": True})
            assert initialized == [session["id"]], initialized
        finally:
            sessions_module.scaffold.scaffold_env = original_scaffold
            svc._initialize = original_initialize  # type: ignore[method-assign]
            await svc.db.close()
    print("PASS test_scaffold_retries_initialization_only_for_variants")


async def test_delete_refuses_working_dir_outside_session_root() -> None:
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        cfg = tmp / "dreadgoad.yaml"
        cfg.write_text(_YAML)
        svc = await _svc(tmp)
        try:
            session = await svc.create_session(str(cfg), _ENV)
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


def _range_catalog() -> list[dict[str, object]]:
    return [
        {
            "name": "SERVICE",
            "display_name": "SERVICE",
            "generated": False,
            "variant_supported": False,
            "provider_settings": {
                "aws": {
                    "deployment": "service-deployment",
                    "scaffold_profile": "template",
                    "template_environment": "service-aws",
                    "default_region": "us-east-2",
                    "network": {"cidr": "10.50.0.0/16", "editable": False},
                },
                "azure": {
                    "deployment": "service-deployment",
                    "scaffold_profile": "template",
                    "template_environment": "service-dev",
                    "default_region": "centralus",
                    "network": {"cidr": "10.50.0.0/16", "editable": False},
                },
            },
        },
        {
            "name": "GOAD",
            "display_name": "GOAD",
            "generated": False,
            "variant_supported": True,
            "provider_settings": {
                "aws": {
                    "deployment": "goad-deployment",
                    "scaffold_profile": "active-directory",
                    "template_environment": "staging",
                    "default_region": "us-west-1",
                    "network": {"editable": True},
                },
                "azure": {
                    "deployment": "goad-deployment",
                    "scaffold_profile": "active-directory",
                    "template_environment": "test",
                    "default_region": "centralus",
                    "network": {"editable": True},
                },
            },
        },
    ]


def test_range_creation_plan_is_pure_and_complete() -> None:
    catalog = _range_catalog()
    original = copy.deepcopy(catalog)

    plan = sessions_module._resolve_range_creation_plan(
        catalog,
        sessions_module.RangeCreationRequest(
            range_name="SERVICE", provider="azure", env_name=" unit-service "
        ),
    )
    assert plan.range_name == "SERVICE"
    assert plan.env_name == "unit-service"
    assert plan.deployment == "service-deployment"
    assert plan.region == "centralus"
    assert plan.vpc_cidr == "10.50.0.0/16"
    assert plan.variant_target is None
    assert plan.default_label == "unit-service · SERVICE"
    assert plan.environment_fields() == {
        "lab": "SERVICE",
        "provider": "azure",
        "deployment": "service-deployment",
        "region": "centralus",
        "vpc_cidr": "10.50.0.0/16",
        "variant": False,
    }

    variant = sessions_module._resolve_range_creation_plan(
        catalog,
        sessions_module.RangeCreationRequest(
            range_name="GOAD",
            provider="aws",
            env_name="variant-one",
            customization="randomized",
            vpc_cidr="10.77.0.0/16",
        ),
    )
    assert variant.variant_source == "ad/GOAD"
    assert variant.variant_target == "ad/GOAD-variant-one"
    assert variant.environment_fields()["variant_name"] == "variant-one"

    fields = plan.environment_fields()
    fields["region"] = "mutated"
    assert plan.region == "centralus", "rendered fields must not mutate the plan"
    assert catalog == original, "planning must not mutate catalog metadata"
    print("PASS test_range_creation_plan_is_pure_and_complete")


def test_range_creation_plan_rejects_malformed_catalog_metadata() -> None:
    malformed = {
        "name": "BROKEN",
        "display_name": "Broken",
        "generated": False,
        "variant_supported": False,
        "provider_settings": {
            "azure": {
                "deployment": "service-deployment",
                "default_region": "centralus",
                "network": ["10.50.0.0/16"],
            }
        },
    }
    try:
        sessions_module._resolve_range_creation_plan(
            [malformed],
            sessions_module.RangeCreationRequest(
                range_name="BROKEN", provider="azure", env_name="broken-one"
            ),
        )
        raise AssertionError("accepted non-mapping network metadata")
    except ValueError as exc:
        assert "invalid network settings" in str(exc), exc
    print("PASS test_range_creation_plan_rejects_malformed_catalog_metadata")


async def test_create_range_session_scaffolds_service_from_explicit_metadata() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        original_discover = sessions_module.labs.discover_labs
        original_scaffold = sessions_module.scaffold.scaffold_env
        calls: list[tuple[tuple[object, ...], dict[str, object]]] = []

        async def discovered():  # noqa: ANN202
            return _range_catalog()

        async def scaffolded(*args: object, **kwargs: object):
            calls.append((args, kwargs))
            return True, "prepared"

        sessions_module.labs.discover_labs = discovered
        sessions_module.scaffold.scaffold_env = scaffolded
        try:
            session = await svc.create_range_session("SERVICE", "azure", "unit-service")
            assert session["label"] == "unit-service · SERVICE", session
            assert session["snapshot"]["provider"] == "azure", session
            assert session["snapshot"].get("deployment") == "service-deployment"
            assert session["snapshot"]["vpc_cidr"] == "10.50.0.0/16"
            assert len(calls) == 1, calls
            assert calls[0][1]["deployment"] == "service-deployment"
            assert calls[0][1]["vpc_cidr"] == "10.50.0.0/16"
            assert calls[0][1]["variant"] is False

            import yaml

            config_path = pathlib.Path(session["anchor"]["config_path"])
            data = yaml.safe_load(config_path.read_text())
            env = data["environments"]["unit-service"]
            assert "provider" not in data, "managed config must not imply AWS"
            assert env["lab"] == "SERVICE"
            assert env["provider"] == "azure"
            assert env["deployment"] == "service-deployment"
            assert env["variant"] is False

            aws_session = await svc.create_range_session(
                "SERVICE", "aws", "unit-service-aws"
            )
            assert aws_session["snapshot"]["provider"] == "aws", aws_session
            assert aws_session["snapshot"]["region"] == "us-east-2", aws_session
            assert aws_session["snapshot"]["vpc_cidr"] == "10.50.0.0/16"
            assert len(calls) == 2, calls
            assert calls[1][1]["provider"] == "aws"
            assert calls[1][1]["deployment"] == "service-deployment"
            aws_config = pathlib.Path(aws_session["anchor"]["config_path"])
            aws_data = yaml.safe_load(aws_config.read_text())
            aws_env = aws_data["environments"]["unit-service-aws"]
            assert aws_env["provider"] == "aws"
            assert aws_env["region"] == "us-east-2"
            assert aws_env["lab"] == "SERVICE"
        finally:
            sessions_module.labs.discover_labs = original_discover
            sessions_module.scaffold.scaffold_env = original_scaffold
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved
    print("PASS test_create_range_session_scaffolds_service_from_explicit_metadata")


async def test_create_range_session_builds_supported_variant() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        original_discover = sessions_module.labs.discover_labs
        original_scaffold = sessions_module.scaffold.scaffold_env
        seen: dict[str, object] = {}

        async def discovered():  # noqa: ANN202
            return _range_catalog()

        async def scaffolded(*_args: object, **kwargs: object):
            seen.update(kwargs)
            return True, "prepared"

        sessions_module.labs.discover_labs = discovered
        sessions_module.scaffold.scaffold_env = scaffolded
        try:
            session = await svc.create_range_session(
                "GOAD",
                "aws",
                "unit-goad-variant",
                customization="randomized",
                vpc_cidr="10.77.0.0/16",
            )
            assert session["snapshot"]["region"] == "us-west-1"
            assert session["snapshot"]["lab"] == "ad/GOAD-unit-goad-variant"
            assert seen["variant"] is True
            assert seen["variant_source"] == "ad/GOAD"
            assert seen["variant_target"] == "ad/GOAD-unit-goad-variant"
        finally:
            sessions_module.labs.discover_labs = original_discover
            sessions_module.scaffold.scaffold_env = original_scaffold
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved
    print("PASS test_create_range_session_builds_supported_variant")


async def test_create_range_session_rejects_bad_policy_before_writes() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        original_discover = sessions_module.labs.discover_labs

        async def discovered():  # noqa: ANN202
            return _range_catalog()

        sessions_module.labs.discover_labs = discovered
        try:
            cases = [
                (
                    {"range_name": "SERVICE", "provider": "proxmox"},
                    "provider must be one of",
                ),
                (
                    {
                        "range_name": "SERVICE",
                        "provider": "azure",
                        "customization": "randomized",
                    },
                    "does not support randomized",
                ),
                (
                    {
                        "range_name": "SERVICE",
                        "provider": "azure",
                        "vpc_cidr": "10.99.0.0/16",
                    },
                    "requires VPC/VNet CIDR",
                ),
                (
                    {
                        "range_name": "GOAD",
                        "provider": "aws",
                        "vpc_cidr": "10.2.0.0/24",
                    },
                    "requires an IPv4 /16",
                ),
                (
                    {
                        "range_name": "GOAD",
                        "provider": "aws",
                        "vpc_cidr": "127.0.0.0/16",
                    },
                    "use RFC 1918",
                ),
            ]
            for index, (kwargs, expected) in enumerate(cases):
                try:
                    await svc.create_range_session(
                        env_name=f"unit-rejected-{index}", **kwargs
                    )
                    raise AssertionError(f"accepted {kwargs}")
                except ValueError as exc:
                    assert expected in str(exc), (kwargs, exc)
            assert await svc.list_sessions() == []
            assert list(paths.configs_root().glob("*.yaml")) == []
        finally:
            sessions_module.labs.discover_labs = original_discover
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved
    print("PASS test_create_range_session_rejects_bad_policy_before_writes")


async def test_create_range_session_rolls_back_failed_scaffold() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        original_discover = sessions_module.labs.discover_labs
        original_scaffold = sessions_module.scaffold.scaffold_env

        async def discovered():  # noqa: ANN202
            return _range_catalog()

        async def failed(*_args: object, **_kwargs: object):
            return False, "template is incomplete"

        sessions_module.labs.discover_labs = discovered
        sessions_module.scaffold.scaffold_env = failed
        try:
            try:
                await svc.create_range_session(
                    "SERVICE", "azure", "unit-failed-service"
                )
                raise AssertionError("failed scaffold unexpectedly created a session")
            except ValueError as exc:
                assert "template is incomplete" in str(exc), exc
            assert await svc.list_sessions() == []
            assert list(paths.configs_root().glob("*.yaml")) == []
            assert not any(svc.sessions_root.iterdir()), "session directory survived"
        finally:
            sessions_module.labs.discover_labs = original_discover
            sessions_module.scaffold.scaffold_env = original_scaffold
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved
    print("PASS test_create_range_session_rolls_back_failed_scaffold")


async def test_create_range_session_rolls_back_scaffold_exception() -> None:
    saved = os.environ.get("DREADGOAD_CONSOLE_STATE_ROOT")
    with tempfile.TemporaryDirectory() as d:
        tmp = pathlib.Path(d)
        os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = str(tmp / "state")
        svc = await _svc(tmp)
        original_discover = sessions_module.labs.discover_labs
        original_scaffold = sessions_module.scaffold.scaffold_env
        boom = RuntimeError("scaffolder crashed")

        async def discovered():  # noqa: ANN202
            return _range_catalog()

        async def failed(*_args: object, **_kwargs: object):
            raise boom

        sessions_module.labs.discover_labs = discovered
        sessions_module.scaffold.scaffold_env = failed
        try:
            try:
                await svc.create_range_session(
                    "SERVICE", "azure", "unit-crashed-service"
                )
                raise AssertionError(
                    "scaffold exception unexpectedly created a session"
                )
            except RuntimeError as exc:
                assert exc is boom, exc
            assert await svc.list_sessions() == []
            assert list(paths.configs_root().glob("*.yaml")) == []
            assert not any(svc.sessions_root.iterdir()), "session directory survived"
        finally:
            sessions_module.labs.discover_labs = original_discover
            sessions_module.scaffold.scaffold_env = original_scaffold
            await svc.db.close()
            os.environ.pop("DREADGOAD_CONSOLE_STATE_ROOT", None)
            if saved is not None:
                os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = saved
    print("PASS test_create_range_session_rolls_back_scaffold_exception")


async def _main() -> None:
    test_range_creation_plan_is_pure_and_complete()
    test_range_creation_plan_rejects_malformed_catalog_metadata()
    await test_create_attach_session()
    await test_service_repairs_existing_session_directory_modes()
    await test_delete_session_removes_dir_and_rows()
    await test_create_session_records_initialization_results()
    await test_scaffold_retries_initialization_only_for_variants()
    await test_delete_refuses_working_dir_outside_session_root()
    await test_create_new_env_writes_yaml_and_backs_up()
    await test_greenfield_seeds_infra_only()
    test_default_label_names_the_config_and_env()
    await test_create_config_session_writes_a_config_and_attaches()
    await test_create_config_session_refuses_a_taken_name()
    await test_create_config_session_rolls_back_on_failure()
    await test_create_range_session_scaffolds_service_from_explicit_metadata()
    await test_create_range_session_builds_supported_variant()
    await test_create_range_session_rejects_bad_policy_before_writes()
    await test_create_range_session_rolls_back_failed_scaffold()
    await test_create_range_session_rolls_back_scaffold_exception()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
