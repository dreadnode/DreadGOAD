"""REST endpoint tests via FastAPI TestClient (Phase 2: T2.2).

Standalone:  python console/backend/tests/test_server_rest.py
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

os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = _TMP

from fastapi import WebSocketDisconnect  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402

from console.backend.db import Database  # noqa: E402
from console.backend.server import (  # noqa: E402
    app,
    reconcile_interrupted,
    ws_origin_allowed,
)

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

        # config exposes the key-set indicator
        assert "api_key_set" in client.get("/api/config").json()
        print("PASS config")

        # settings: store a key into a throwaway env var (in-memory, not persisted)
        r = client.post(
            "/api/settings",
            json={"api_key": "sk-test-xyz", "api_key_env": "DG_TEST_API_KEY"},
        )
        assert r.status_code == 200 and r.json()["api_key_env"] == "DG_TEST_API_KEY", (
            r.text
        )
        assert os.environ.get("DG_TEST_API_KEY") == "sk-test-xyz"
        # key-shaped env-var name but unset, no key provided → 400
        assert (
            client.post(
                "/api/settings", json={"api_key_env": "DG_UNSET_API_KEY"}
            ).status_code
            == 400
        )
        # non-key env names are rejected (can't clobber PATH/LD_PRELOAD)
        for bad in ("PATH", "LD_PRELOAD", "HOME", "DG_TEST"):
            assert (
                client.post(
                    "/api/settings", json={"api_key": "x", "api_key_env": bad}
                ).status_code
                == 400
            ), bad
        assert os.environ.get("PATH") != "x", "PATH must never be overwritten"
        os.environ.pop("DG_TEST_API_KEY", None)
        print("PASS settings")

        # environments dropdown source
        envs = client.get(f"/api/environments?config_path={cfg}").json()
        assert envs["environments"] == ["dev"], envs
        assert envs["provider"] == "aws", envs
        # bad path → 400
        assert (
            client.get(
                "/api/environments?config_path=/tmp/nope-abc-123.yaml"
            ).status_code
            == 400
        )
        # non-dict YAML (points at a non-config file) → 400, not 500
        notcfg = pathlib.Path(_TMP) / "notcfg.yaml"
        notcfg.write_text("just a plain string, not a mapping\n")
        assert client.get(f"/api/environments?config_path={notcfg}").status_code == 400
        print("PASS environments")

        # command catalog (drives the frontend autocomplete)
        cat = client.get("/api/commands").json()["commands"]
        assert len(cat) == 14, cat
        names = {c["name"] for c in cat}
        assert "/up" in names and "/destroy" in names, names
        up = next(c for c in cat if c["name"] == "/up")
        assert up["dispatch"] == "agent" and up["description"], up
        assert next(c for c in cat if c["name"] == "/instances")["dispatch"] == "direct"
        print("PASS command catalog")

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

        # create a NEW environment in the config, then attach (mode="new")
        r = client.post(
            "/api/sessions",
            json={
                "mode": "new",
                "config_path": str(cfg),
                "env": "redteam",
                "env_fields": {
                    "variant": True,
                    "variant_source": "ad/GOAD",
                    "variant_target": "ad/GOAD-redteam",
                    "variant_name": "redteam",
                    "vpc_cidr": "10.100.0.0/16",
                },
            },
        )
        assert r.status_code == 200, r.text
        assert r.json()["snapshot"]["lab"] == "ad/GOAD-redteam", r.json()
        # the env was written into the config → now selectable via the dropdown
        envs2 = client.get(f"/api/environments?config_path={cfg}").json()[
            "environments"
        ]
        assert "redteam" in envs2 and "dev" in envs2, envs2
        print("PASS create new environment")

        # list
        lst = client.get("/api/sessions").json()["sessions"]
        assert any(x["id"] == sid for x in lst), lst
        print("PASS list sessions")

        # get one
        assert client.get(f"/api/sessions/{sid}").json()["id"] == sid
        assert client.get("/api/sessions/nope").status_code == 404
        print("PASS get session + 404")

        # set model (no live agent → persists on the session)
        r = client.put(
            f"/api/sessions/{sid}/model", json={"model": "openrouter/foo/bar"}
        )
        assert r.status_code == 200 and r.json()["model"] == "openrouter/foo/bar", (
            r.text
        )
        assert (
            client.get(f"/api/sessions/{sid}").json()["model"] == "openrouter/foo/bar"
        )
        assert (
            client.put(f"/api/sessions/{sid}/model", json={"model": " "}).status_code
            == 400
        )
        assert (
            client.put("/api/sessions/nope/model", json={"model": "x"}).status_code
            == 404
        )
        print("PASS set model + 400 + 404")

        # range read (seeded topology, infra-only since ad/GOAD is the base lab)
        rng = client.get(f"/api/ranges/{sid}").json()
        assert any(h["id"] == "attackbox" for h in rng["hosts"]), rng
        print("PASS range read")

        # delete
        assert client.delete(f"/api/sessions/{sid}").status_code == 200
        assert client.get(f"/api/sessions/{sid}").status_code == 404
        print("PASS delete session")

        test_ws_origin_allowed()
        test_ws_rejects_cross_origin(client)

    asyncio.run(test_reconcile_interrupted())
    print("ALL PASS")


def test_ws_origin_allowed() -> None:
    """Only loopback origins (any port) may open the chat socket."""
    # No Origin → not a browser, never had cross-origin authority.
    assert ws_origin_allowed(None)
    for good in (
        "http://localhost:7331",
        "http://127.0.0.1:5173",  # --dev: vite's port, proxied through
        "https://localhost",
        "http://[::1]:7331",
        "HTTP://LocalHost:7331",  # header case is not significant
    ):
        assert ws_origin_allowed(good), good
    for bad in (
        "http://evil.com",
        "https://localhost.evil.com",  # suffix trick
        "http://evil.com/localhost",
        "http://127.0.0.1.evil.com",
        "http://notlocalhost",
        "",
    ):
        assert not ws_origin_allowed(bad), bad
    print("PASS test_ws_origin_allowed")


def test_ws_rejects_cross_origin(client: TestClient) -> None:
    """A page on another origin can't drive the console over the socket."""
    accepted = False
    try:
        with client.websocket_connect(
            "/ws/chat", headers={"origin": "http://evil.com"}
        ):
            accepted = True
    except WebSocketDisconnect as exc:
        assert exc.code == 1008, f"expected 1008 policy violation, got {exc.code}"
    assert not accepted, "cross-origin websocket handshake was accepted"
    # The console's own origin still connects.
    with client.websocket_connect(
        "/ws/chat", headers={"origin": "http://localhost:7331"}
    ):
        pass
    print("PASS test_ws_rejects_cross_origin")


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
