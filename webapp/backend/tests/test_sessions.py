"""Tests for SessionService (Phase 2: T2.2).

Standalone:  python webapp/backend/tests/test_sessions.py
"""

from __future__ import annotations

import asyncio
import os
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from webapp.backend.db import Database  # noqa: E402
from webapp.backend.sessions import SessionService  # noqa: E402

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


async def _svc(tmp: pathlib.Path) -> SessionService:
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


async def _main() -> None:
    await test_create_attach_session()
    await test_delete_session_removes_dir_and_rows()
    await test_create_new_env_writes_yaml_and_backs_up()
    print("ALL PASS")


if __name__ == "__main__":
    asyncio.run(_main())
