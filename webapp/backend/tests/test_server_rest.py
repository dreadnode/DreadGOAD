"""REST endpoint tests via FastAPI TestClient (Phase 2: T2.2).

Standalone:  python webapp/backend/tests/test_server_rest.py
Requires fastapi + httpx (installed in the project venv).
"""

from __future__ import annotations

import pathlib
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

_TMP = tempfile.mkdtemp(prefix="dg-rest-")
# Isolate DB + session dirs before importing the app.
import os  # noqa: E402

os.environ["DREADGOAD_WEBAPP_STATE_ROOT"] = _TMP

from fastapi.testclient import TestClient  # noqa: E402

from webapp.backend.server import app  # noqa: E402

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

    print("ALL PASS")


if __name__ == "__main__":
    main()
