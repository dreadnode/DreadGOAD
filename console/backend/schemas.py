"""Shared shapes for the console's persisted JSON documents."""

from __future__ import annotations

import typing as t


class SessionAnchor(t.TypedDict):
    """Stable range identity attached to a console session."""

    config_path: str
    env: str


class AWSSelectors(t.TypedDict):
    """Non-secret AWS selectors retained in a session snapshot."""

    profile: str | None


class AzureSelectors(t.TypedDict):
    """Non-secret Azure connection selectors retained in a snapshot."""

    ssh_key: str | None
    ssh_user: str | None


class SessionSnapshotRequired(t.TypedDict):
    """Provider and deployment facts derived for a session."""

    provider: str | None
    region: str | None
    lab: str | None
    variant_name: str | None
    vpc_cidr: str | None
    attack_box: str | None


class SessionSnapshot(SessionSnapshotRequired, total=False):
    """Session facts supplemented after cloud discovery."""

    account: str | None
    group: str | None
    aws: AWSSelectors
    azure: AzureSelectors


class SessionDocumentRequired(t.TypedDict):
    """Fields present on both current and legacy session documents."""

    id: str
    label: str
    status: str
    anchor: SessionAnchor
    snapshot: SessionSnapshot
    session_dir: str
    created_at: str
    updated_at: str


class SessionDocument(SessionDocumentRequired, total=False):
    """Session document; old persisted rows may predate model selection."""

    model: str | None


class RangeHostRequired(t.TypedDict):
    """Fields present on every seeded topology host."""

    id: str
    hostname: str
    role: str
    source: str
    status: str
    health: str
    domain: str | None
    ip_private: str | None
    ip_public: str | None
    cloud_id: str | None
    cloud_name: str | None
    last_checked_at: str | None


class RangeHost(RangeHostRequired, total=False):
    """Topology host; old persisted rows may predate optional metadata."""

    key: str
    os: str | None


def new_range_host(
    host_id: str,
    key: str,
    hostname: str,
    role: str,
    source: str,
    domain: str | None,
    operating_system: str | None = None,
) -> RangeHost:
    """Build a topology host with consistent initial dynamic state."""
    return {
        "id": host_id,
        "key": key,
        "hostname": hostname,
        "role": role,
        "source": source,
        "domain": domain,
        "os": operating_system,
        "status": "unknown",
        "health": "unknown",
        "ip_private": None,
        "ip_public": None,
        "cloud_id": None,
        "cloud_name": None,
        "last_checked_at": None,
    }


class RangeDocumentRequired(t.TypedDict):
    """Fields present on seeded and persisted topologies."""

    hosts: list[RangeHost]
    edges: list[dict[str, t.Any]]
    layout: dict[str, dict[str, int]]
    last_checked_at: str | None


class RangeDocument(RangeDocumentRequired, total=False):
    """Persisted topology; ``session_id`` is added after initial seeding."""

    session_id: str


class PersistedRangeRequired(RangeDocumentRequired):
    """Range fields normalized by the persistence layer."""

    layout_revision: int


class PersistedRangeDocument(PersistedRangeRequired, total=False):
    """Range document returned from SQLite."""

    session_id: str


class EventRecord(t.TypedDict):
    """One persisted event before its payload is flattened for WebSocket use."""

    seq: int
    kind: str
    ts: str
    payload: dict[str, t.Any]
