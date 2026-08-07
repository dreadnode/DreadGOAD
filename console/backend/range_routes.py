"""Range topology and layout HTTP routes."""

from __future__ import annotations

import math
import typing as t

from fastapi import APIRouter, HTTPException, Request

router = APIRouter()

LAYOUT_MAX_NODES = 1_000
LAYOUT_MAX_NODE_ID_CHARS = 128
LAYOUT_MAX_ABS_COORDINATE = 1_000_000


@router.get("/api/ranges/{session_id}")
async def get_range(request: Request, session_id: str) -> dict[str, t.Any]:
    """Return the topology rendered by the RangeView."""
    rng = await request.app.state.db.get_range(session_id)
    if rng is None:
        raise HTTPException(status_code=404, detail="range not found")
    return rng


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
