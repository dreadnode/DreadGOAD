"""REST endpoint tests via FastAPI TestClient (Phase 2: T2.2).

Standalone:  python console/backend/tests/test_server_rest.py
Requires fastapi + httpx (installed in the project venv).
"""

from __future__ import annotations

import asyncio
import json
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

from console.backend import chat, commands  # noqa: E402
from console.backend.db import Database  # noqa: E402
from console.backend.server import (  # noqa: E402
    WS_MAX_CONTENT_CHARS,
    WS_MAX_MESSAGE_CHARS,
    WS_MAX_SESSION_ID_CHARS,
    app,
    parse_ws_message,
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
        # The endpoint must expose the whole registry — compared against it
        # rather than a literal, which only ever needs bumping.
        assert len(cat) == len(commands.REGISTRY), cat
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
        assert rng["layout_revision"] == 0, rng
        print("PASS range read")

        # Layout updates use optimistic revisions: the first write preserves
        # the rest of the range, and a repeated revision cannot overwrite it.
        node_id = rng["hosts"][0]["id"]
        first_layout = {node_id: {"x": 101.4, "y": 202.6}}
        saved = client.put(
            f"/api/ranges/{sid}/layout",
            json={"layout": first_layout, "revision": 0},
        )
        assert saved.status_code == 200 and saved.json()["layout_revision"] == 1
        after_layout = client.get(f"/api/ranges/{sid}").json()
        assert after_layout["layout"][node_id] == {"x": 101, "y": 203}
        assert after_layout["hosts"] == rng["hosts"], "layout save replaced topology"

        stale = client.put(
            f"/api/ranges/{sid}/layout",
            json={"layout": {node_id: {"x": 1, "y": 2}}, "revision": 0},
        )
        assert stale.status_code == 409, stale.text
        assert stale.json()["detail"]["layout_revision"] == 1, stale.text
        assert client.get(f"/api/ranges/{sid}").json()["layout"] == after_layout["layout"]

        for invalid_layout in (
            {"layout": [], "revision": 1},
            {"layout": {}, "revision": -1},
            {"layout": {node_id: {"x": True, "y": 2}}, "revision": 1},
            {"layout": {node_id: {"x": 1}}, "revision": 1},
            {"layout": {node_id: {"x": 10**400, "y": 2}}, "revision": 1},
        ):
            response = client.put(
                f"/api/ranges/{sid}/layout", json=invalid_layout
            )
            assert response.status_code == 400, (invalid_layout, response.text)
        print("PASS versioned layout update + validation")

        # delete
        assert client.delete(f"/api/sessions/{sid}").status_code == 200
        assert client.get(f"/api/sessions/{sid}").status_code == 404
        print("PASS delete session")

        test_ws_origin_allowed()
        test_parse_ws_message_validation()
        test_ws_rejects_cross_origin(client)
        test_ws_invalid_message_does_not_close_connection(client)
        test_ws_reports_rejected_busy_turn(client)

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


def test_parse_ws_message_validation() -> None:
    """Only the three bounded protocol shapes reach the chat runtime."""
    valid = (
        ('{"session_id":"s-1","content":" hello "}', "message", "hello"),
        ('{"type":"message","session_id":"s-1","content":"hello"}', "message", "hello"),
        ('{"type":"resume","session_id":"s-1"}', "resume", None),
        ('{"type":"cancel","session_id":"s-1"}', "cancel", None),
    )
    for raw, expected_type, expected_content in valid:
        message, error, session_id = parse_ws_message(raw)
        assert error is None and session_id == "s-1", (message, error, session_id)
        assert message is not None and message["type"] == expected_type, message
        assert message.get("content") == expected_content, message

    invalid = (
        ("not json", "valid JSON"),
        ("[]", "JSON object"),
        ("null", "JSON object"),
        ('{"content":"hello"}', "session_id"),
        ('{"session_id":[],"content":"hello"}', "session_id"),
        ('{"session_id":"s-1","type":false}', "type must"),
        ('{"session_id":"s-1","type":"unknown"}', "unknown message type"),
        ('{"session_id":"s-1","content":7}', "content must"),
        ('{"session_id":"s-1","content":"  "}', "must not be empty"),
        ('{"session_id":"s-1","type":"cancel","content":"x"}', "unexpected field"),
        (
            json.dumps(
                {
                    "session_id": "s" * (WS_MAX_SESSION_ID_CHARS + 1),
                    "type": "resume",
                }
            ),
            "session_id is too long",
        ),
    )
    for raw, expected_error in invalid:
        message, error, _ = parse_ws_message(raw)
        assert message is None and error is not None, (raw, message, error)
        assert expected_error in error, (raw, error)

    oversized_content = json.dumps(
        {"session_id": "s-1", "content": "x" * (WS_MAX_CONTENT_CHARS + 1)}
    )
    assert parse_ws_message(oversized_content)[1] == "content is too large"
    assert (
        parse_ws_message(" " * (WS_MAX_MESSAGE_CHARS + 1))[1]
        == "message is too large"
    )
    print("PASS test_parse_ws_message_validation")


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


def test_ws_invalid_message_does_not_close_connection(client: TestClient) -> None:
    """Bad frames report errors while later valid frames still dispatch."""
    dispatched: list[tuple[str, str]] = []
    original = chat.dispatch

    def record_dispatch(app_, session_id, content):  # noqa: ANN001, ANN202
        dispatched.append((session_id, content))
        return object()  # Any non-None value represents accepted admission here.

    chat.dispatch = record_dispatch
    try:
        with client.websocket_connect(
            "/ws/chat", headers={"origin": "http://localhost:7331"}
        ) as websocket:
            websocket.send_text("[]")
            assert websocket.receive_json() == {
                "kind": "error",
                "message": "message must be a JSON object",
            }

            websocket.send_json({"session_id": "s-valid", "content": 7})
            assert websocket.receive_json() == {
                "session_id": "s-valid",
                "kind": "error",
                "message": "content must be a string",
            }

            # The same socket remains usable after both validation failures.
            websocket.send_json({"session_id": "s-valid", "content": " go "})
            websocket.send_json({"session_id": "s-valid", "type": "wat"})
            assert websocket.receive_json()["message"] == "unknown message type: wat"
            assert dispatched == [("s-valid", "go")], dispatched
        print("PASS test_ws_invalid_message_does_not_close_connection")
    finally:
        chat.dispatch = original


def test_ws_reports_rejected_busy_turn(client: TestClient) -> None:
    """A refused second turn gets a visible, non-terminal error event."""
    original = chat.dispatch
    chat.dispatch = lambda app_, sid_, content: None
    try:
        with client.websocket_connect(
            "/ws/chat", headers={"origin": "http://localhost:7331"}
        ) as websocket:
            websocket.send_json({"session_id": "busy-session", "content": "second"})
            event = websocket.receive_json()
            assert event == {
                "session_id": "busy-session",
                "kind": "error",
                "message": chat.TURN_BUSY_MESSAGE,
            }, event
        print("PASS test_ws_reports_rejected_busy_turn")
    finally:
        chat.dispatch = original


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
