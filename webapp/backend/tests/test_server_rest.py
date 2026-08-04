"""REST endpoint tests via FastAPI TestClient (Phase 2: T2.2).

Standalone:  python webapp/backend/tests/test_server_rest.py
Requires fastapi + httpx (installed in the project venv).
"""

from __future__ import annotations

import asyncio
import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

_TMP = tempfile.mkdtemp(prefix="dg-rest-")
# Isolate DB + session dirs before importing the app.
import os  # noqa: E402

os.environ["DREADGOAD_WEBAPP_STATE_ROOT"] = _TMP

from fastapi.testclient import TestClient  # noqa: E402

from webapp.backend.db import Database  # noqa: E402
from webapp.backend.server import app, reconcile_interrupted  # noqa: E402

_YAML = """\
provider: aws
region: us-west-2
environments:
  dev:
    variant_source: ad/GOAD
    vpc_cidr: "10.0.0.0/16"
"""


def main() -> None:
    cfg = pathlib.Path(_TMP) / "dreadgoad.yaml"
    cfg.write_text(_YAML)

    with TestClient(app) as client:
        # health
        assert client.get("/api/health").json()["status"] == "ok"

        # create (attach)
        r = client.post("/api/sessions", json={"config_path": str(cfg), "env": "dev"})
        assert r.status_code == 200, r.text
        s = r.json()
        sid = s["id"]
        assert s["snapshot"]["provider"] == "aws", s
        print("PASS create session")

        # missing env → 400
        assert (
            client.post("/api/sessions", json={"config_path": str(cfg)}).status_code
            == 400
        )
        print("PASS create requires env")

        # bad config path → 400 (not 500)
        assert (
            client.post(
                "/api/sessions",
                json={"config_path": "/tmp/nope-xyz-123.yaml", "env": "dev"},
            ).status_code
            == 400
        )
        print("PASS create bad config path → 400")

        # unknown env in a valid file → 400 (not a silently-broken session)
        assert (
            client.post(
                "/api/sessions", json={"config_path": str(cfg), "env": "ghost"}
            ).status_code
            == 400
        )
        print("PASS create unknown env → 400")

        # list
        lst = client.get("/api/sessions").json()["sessions"]
        assert any(x["id"] == sid for x in lst), lst
        print("PASS list sessions")

        # get one
        assert client.get(f"/api/sessions/{sid}").json()["id"] == sid
        assert client.get("/api/sessions/nope").status_code == 404
        print("PASS get session + 404")

        # range read (seeded topology, infra-only since ad/GOAD is the base lab)
        rng = client.get(f"/api/ranges/{sid}").json()
        assert any(h["id"] == "attackbox" for h in rng["hosts"]), rng
        print("PASS range read")

        # delete
        assert client.delete(f"/api/sessions/{sid}").status_code == 200
        assert client.get(f"/api/sessions/{sid}").status_code == 404
        print("PASS delete session")

    asyncio.run(test_reconcile_interrupted())
    print("ALL PASS")


async def test_reconcile_interrupted() -> None:
    """A crash-killed 'provisioning' session is flipped to 'interrupted' (T6.2)."""
    with tempfile.TemporaryDirectory() as d:
        db = await Database(str(pathlib.Path(d) / "state.db")).connect()
        try:
            await db.upsert_session({"id": "s-prov", "status": "provisioning"})
            await db.upsert_session({"id": "s-run", "status": "running"})
            n = await reconcile_interrupted(db)
            assert n == 1, f"expected 1 reconciled, got {n}"
            prov = await db.get_session("s-prov")
            run = await db.get_session("s-run")
            assert prov is not None and prov["status"] == "interrupted", prov
            assert run is not None and run["status"] == "running", (
                "non-provisioning untouched"
            )
            print("PASS test_reconcile_interrupted")
        finally:
            await db.close()


if __name__ == "__main__":
    main()
