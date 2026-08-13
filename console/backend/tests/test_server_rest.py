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
from collections.abc import Iterator

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

_TMP = tempfile.mkdtemp(prefix="dg-rest-")
# Isolate DB + session dirs before importing the app.
import os  # noqa: E402

os.environ["DREADGOAD_CONSOLE_STATE_ROOT"] = _TMP

import pytest  # noqa: E402
from fastapi import WebSocketDisconnect  # noqa: E402
from fastapi.testclient import TestClient  # noqa: E402

from console.backend import chat, chat_runtime, commands  # noqa: E402
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


@pytest.fixture
def client() -> Iterator[TestClient]:
    """A started app for the websocket tests below.

    Entering TestClient runs the lifespan, which is what creates
    ``app.state.db`` — several of these tests patch it. This module doubles as
    a standalone script (see ``main``), where that setup is inline instead;
    the pytest functions had no such fixture and so never ran.
    """
    with TestClient(app) as started:
        yield started


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

        # create a whole new CONFIG, its first env, and a session, in one call
        r = client.post(
            "/api/sessions",
            json={
                "mode": "new_config",
                "config_name": "Azure Lab",
                "provider": "azure",
                "region": "eastus",
                "env": "ops",
                "env_fields": {"variant": False, "vpc_cidr": "10.11.0.0/16"},
            },
        )
        assert r.status_code == 200, r.text
        made = r.json()
        assert made["snapshot"]["provider"] == "azure", made
        assert made["snapshot"]["region"] == "eastus", made
        new_cfg = made["anchor"]["config_path"]
        assert new_cfg.endswith("/configs/azure-lab.yaml"), new_cfg
        # The pre-existing aws config is untouched by the azure one.
        assert (
            client.get(f"/api/environments?config_path={cfg}").json()["provider"]
            == "aws"
        ), "creating a config must not touch another one"
        print("PASS create new config")

        # …and the same name a second time is a 409, not a silent merge
        taken = client.post(
            "/api/sessions",
            json={
                "mode": "new_config",
                "config_name": "Azure Lab",
                "provider": "aws",
                "env": "other",
            },
        )
        assert taken.status_code == 409, (taken.status_code, taken.text)
        print("PASS duplicate config name → 409")

        # providers the CLI has but the console cannot drive are refused up front
        for unsupported in ("proxmox", "ludus", "gcp", ""):
            bad = client.post(
                "/api/sessions",
                json={
                    "mode": "new_config",
                    "config_name": f"x-{unsupported or 'blank'}",
                    "provider": unsupported,
                    "env": "dev",
                },
            )
            assert bad.status_code == 400, (unsupported, bad.status_code, bad.text)
        print("PASS unsupported provider → 400")

        # the config picker sees the default, the new one, and session anchors
        listing = client.get("/api/configs").json()
        by_path = {c["path"]: c for c in listing["configs"]}
        assert new_cfg in by_path, list(by_path)
        assert by_path[new_cfg]["source"] == "managed", by_path[new_cfg]
        assert by_path[new_cfg]["environments"] == ["ops"], by_path[new_cfg]
        # Resolved: entries are canonicalised, and _TMP is a /var symlink here.
        assert str(cfg.resolve()) in by_path, (
            f"a config reached only via a session anchor: {list(by_path)}"
        )
        assert listing["providers"] == ["aws", "azure"], listing["providers"]
        assert set(listing["credential_hints"]) == {"aws", "azure"}, listing
        # Region suggestions for the create form. Sent by the backend so the
        # frontend has no list of its own to drift from.
        assert set(listing["regions"]) == {"aws", "azure"}, listing["regions"]
        for provider, regions in listing["regions"].items():
            assert regions, f"{provider} has no region suggestions"
            assert len(set(regions)) == len(regions), f"{provider} has duplicates"
            assert all(r == r.strip() and r for r in regions), regions
        # The regions this repo already deploys into come first, since those are
        # the ones known to work here.
        assert listing["regions"]["azure"][0] == "centralus", listing["regions"]
        assert listing["regions"]["aws"][0] == "us-west-1", listing["regions"]
        print("PASS config listing")

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
        assert (
            client.get(f"/api/ranges/{sid}").json()["layout"] == after_layout["layout"]
        )

        for invalid_layout in (
            {"layout": [], "revision": 1},
            {"layout": {}, "revision": -1},
            {"layout": {node_id: {"x": True, "y": 2}}, "revision": 1},
            {"layout": {node_id: {"x": 1}}, "revision": 1},
            {"layout": {node_id: {"x": 10**400, "y": 2}}, "revision": 1},
        ):
            response = client.put(f"/api/ranges/{sid}/layout", json=invalid_layout)
            assert response.status_code == 400, (invalid_layout, response.text)
        print("PASS versioned layout update + validation")

        # An active turn owns the session: deletion must not remove persistence
        # or files out from under it.
        runtime = chat_runtime.runtime(sid)
        runtime.turn = chat_runtime.TurnState()
        try:
            busy_delete = client.delete(f"/api/sessions/{sid}")
            assert busy_delete.status_code == 409, busy_delete.text
            assert client.get(f"/api/sessions/{sid}").status_code == 200
        finally:
            runtime.turn = None

        # delete
        assert client.delete(f"/api/sessions/{sid}").status_code == 200
        assert client.get(f"/api/sessions/{sid}").status_code == 404
        print("PASS delete session")

        test_ws_origin_allowed()
        test_parse_ws_message_validation()
        test_ws_rejects_cross_origin(client)
        test_ws_invalid_message_does_not_close_connection(client)
        test_ws_reports_rejected_busy_turn(client)
        test_ws_cancel_reaches_runtime(client)

    asyncio.run(test_reconcile_interrupted())
    print("ALL PASS")


def test_range_read_repairs_missing_config_hosts(client: TestClient) -> None:
    """The GET route must invoke the repair, not just contain the function.

    Without this, deleting the repair call from get_range leaves every other
    test passing: the four topology tests call repair_missing_config_hosts
    directly and never exercise the wiring.

    Reproduces the real failure — a session whose lab config does not exist at
    seed time comes up infra-only, and inventory_sync can never add the hosts
    later — then makes the lab appear and asserts the next read heals it.
    """
    tree = tempfile.mkdtemp()
    cfg = pathlib.Path(tree) / "dreadgoad.yaml"
    cfg.write_text(
        "provider: azure\nregion: centralus\nenvironments:\n"
        "  rt:\n    variant: true\n    variant_target: ad/GOAD-rt\n"
    )

    created = client.post("/api/sessions", json={"config_path": str(cfg), "env": "rt"})
    assert created.status_code == 200, created.text
    sid = created.json()["id"]

    # Seeded before any lab exists in this tree: infra nodes only.
    before = client.get(f"/api/ranges/{sid}").json()
    sources = {h["source"] for h in before["hosts"]}
    assert sources == {"infra"}, before["hosts"]

    # The scaffold/variant generator would produce this after session creation.
    data = pathlib.Path(tree) / "ad" / "GOAD-rt" / "data"
    data.mkdir(parents=True)
    (data / "config.json").write_text(
        json.dumps(
            {
                "lab": {
                    "hosts": {
                        "dc01": {"hostname": "nova", "type": "dc"},
                        "srv02": {"hostname": "vertex", "type": "server"},
                    }
                }
            }
        )
    )

    after = client.get(f"/api/ranges/{sid}").json()
    hosts = {h["id"]: h for h in after["hosts"]}
    assert "nova" in hosts and "vertex" in hosts, hosts.keys()
    assert hosts["nova"]["key"] == "dc01", hosts["nova"]
    # Infra nodes survive the repair.
    assert "attackbox" in hosts and "bastion" in hosts, hosts.keys()

    # Persisted, so a later read does not have to repair again.
    repeat = client.get(f"/api/ranges/{sid}").json()
    assert [h["id"] for h in repeat["hosts"]] == [h["id"] for h in after["hosts"]]

    client.delete(f"/api/sessions/{sid}")


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
        parse_ws_message(" " * (WS_MAX_MESSAGE_CHARS + 1))[1] == "message is too large"
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
    original_get_session = app.state.db.get_session

    async def existing_session(session_id):  # noqa: ANN001, ANN202
        return {"id": session_id}

    def record_dispatch(app_, session_id, content):  # noqa: ANN001, ANN202
        dispatched.append((session_id, content))
        return object()  # Any non-None value represents accepted admission here.

    chat.dispatch = record_dispatch
    app.state.db.get_session = existing_session
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
        app.state.db.get_session = original_get_session


def test_ws_reports_rejected_busy_turn(client: TestClient) -> None:
    """A refused second turn gets a visible, non-terminal error event."""
    original = chat.dispatch
    original_get_session = app.state.db.get_session

    async def existing_session(session_id):  # noqa: ANN001, ANN202
        return {"id": session_id}

    chat.dispatch = lambda app_, sid_, content: None
    app.state.db.get_session = existing_session
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
        app.state.db.get_session = original_get_session


def test_ws_cancel_reaches_runtime(client: TestClient) -> None:
    """The exact frame sent by Esc reaches every registered CLI command."""

    class FakeCommand:
        def __init__(self) -> None:
            self.cancelled = False

        def cancel(self) -> None:
            self.cancelled = True

    sid = "s-ws-cancel"
    first, second = FakeCommand(), FakeCommand()
    runtime = chat_runtime.runtime(sid)
    runtime.turn = chat_runtime.TurnState()
    runtime.running.update({first, second})
    try:
        with client.websocket_connect(
            "/ws/chat", headers={"origin": "http://localhost:7331"}
        ) as websocket:
            websocket.send_json({"type": "cancel", "session_id": sid})
            # A following request/response is a synchronization barrier: the
            # server processed the cancel before it reports this protocol error.
            websocket.send_text("[]")
            assert (
                websocket.receive_json()["message"] == "message must be a JSON object"
            )

        assert first.cancelled and second.cancelled
        assert runtime.turn is not None and runtime.turn.cancelled is True
        print("PASS test_ws_cancel_reaches_runtime")
    finally:
        chat_runtime.runtimes.pop(sid, None)


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
