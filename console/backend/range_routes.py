"""Range topology and layout HTTP routes."""

from __future__ import annotations

import math
import typing as t

from fastapi import APIRouter, HTTPException, Request

from . import hostdetail, topology_sync

router = APIRouter()

LAYOUT_MAX_NODES = 1_000
LAYOUT_MAX_NODE_ID_CHARS = 128
LAYOUT_MAX_ABS_COORDINATE = 1_000_000


@router.get("/api/ranges/{session_id}")
async def get_range(request: Request, session_id: str) -> dict[str, t.Any]:
    """Return the topology rendered by the RangeView.

    A read can repair the topology before returning it — see
    topology_sync.repair_missing_config_hosts for what and why. Writing during a
    GET is not free of concerns, but the alternative is a range that is
    permanently missing its hosts unless the operator knows to press something,
    and the repair is self-limiting: it fires only while the topology has no
    config hosts and the lab config exists, which one pass makes false.
    """
    rng = await request.app.state.db.get_range(session_id)
    if rng is None:
        raise HTTPException(status_code=404, detail="range not found")
    return await topology_sync.repair_missing_config_hosts(request.app, session_id, rng)


@router.get("/api/ranges/{session_id}/hosts/{node_id}")
async def get_host_detail(
    request: Request, session_id: str, node_id: str
) -> dict[str, t.Any]:
    """Disks and network interfaces attached to one host.

    Separate from the range read because it costs cloud calls: it is fetched
    when an operator opens a node, not on every poll of the topology.
    """
    db = request.app.state.db
    session = await db.get_session(session_id)
    rng = await db.get_range(session_id)
    if session is None or rng is None:
        raise HTTPException(status_code=404, detail="range not found")
    try:
        return await hostdetail.host_detail(session, rng, node_id)
    except hostdetail.HostDetailUnavailable as exc:
        # 409, not 500: the request was well-formed and the answer is simply
        # not available yet — usually a host that has not been deployed. The
        # panel renders the reason, so it has to be a sentence.
        raise HTTPException(status_code=409, detail=str(exc)) from exc


def _normalize_layout(body: dict[str, t.Any]) -> tuple[dict[str, dict[str, int]], int]:
    """Validate and normalize a revisioned layout request."""
    if set(body) - {"layout", "revision"}:
        raise HTTPException(status_code=400, detail="unexpected layout fields")

    revision = body.get("revision")
    if isinstance(revision, bool) or not isinstance(revision, int) or revision < 0:
        raise HTTPException(
            status_code=400, detail="revision must be a non-negative integer"
        )

    raw_layout = body.get("layout")
    if not isinstance(raw_layout, dict):
        raise HTTPException(status_code=400, detail="layout must be an object")
    if len(raw_layout) > LAYOUT_MAX_NODES:
        raise HTTPException(status_code=400, detail="layout has too many nodes")

    layout: dict[str, dict[str, int]] = {}
    for node_id, raw_position in raw_layout.items():
        if (
            not isinstance(node_id, str)
            or not node_id.strip()
            or len(node_id) > LAYOUT_MAX_NODE_ID_CHARS
        ):
            raise HTTPException(status_code=400, detail="invalid layout node id")
        if not isinstance(raw_position, dict) or set(raw_position) != {"x", "y"}:
            raise HTTPException(
                status_code=400, detail=f"invalid position for node {node_id}"
            )
        normalized: dict[str, int] = {}
        for axis in ("x", "y"):
            value = raw_position[axis]
            if (
                isinstance(value, bool)
                or not isinstance(value, (int, float))
                or (isinstance(value, float) and not math.isfinite(value))
                or abs(value) > LAYOUT_MAX_ABS_COORDINATE
            ):
                raise HTTPException(
                    status_code=400,
                    detail=f"invalid {axis} coordinate for node {node_id}",
                )
            normalized[axis] = round(value)
        layout[node_id] = normalized
    return layout, revision


@router.put("/api/ranges/{session_id}/layout")
async def save_layout(
    request: Request, session_id: str, body: dict[str, t.Any]
) -> dict[str, t.Any]:
    """Persist revision-protected RangeView node positions."""
    layout, revision = _normalize_layout(body)
    result = await request.app.state.db.update_range_layout(
        session_id, layout, revision
    )
    if result is None:
        raise HTTPException(status_code=404, detail="range not found")
    saved, current_revision = result
    if not saved:
        raise HTTPException(
            status_code=409,
            detail={
                "message": "stale layout revision",
                "layout_revision": current_revision,
            },
        )
    return {"ok": True, "layout_revision": current_revision}
