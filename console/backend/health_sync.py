"""Health report parsing and per-host range overlays."""

from __future__ import annotations

import json
import typing as t


def parse_health_report(output: str) -> dict[str, t.Any] | None:
    """Extract a health report from noisy NDJSON or legacy blob output."""
    for line in output.splitlines():
        line = line.strip()
        if not line.startswith("{") or '"checks"' not in line:
            continue
        try:
            parsed = json.loads(line)
        except (ValueError, TypeError):
            continue
        if isinstance(parsed, dict) and "checks" in parsed:
            return parsed
    candidates = [output]
    start, end = output.find("{"), output.rfind("}")
    if 0 <= start < end:
        candidates.append(output[start : end + 1])
    for candidate in candidates:
        try:
            parsed = json.loads(candidate)
        except (ValueError, TypeError):
            continue
        if isinstance(parsed, dict) and "checks" in parsed:
            return parsed
    return None


def host_health_from_report(checks: list[dict[str, t.Any]]) -> dict[str, str]:
    """Aggregate check statuses into one verdict per upper-case host role."""
    statuses: dict[str, set[str]] = {}
    for check in checks:
        host = str(check.get("host") or "").upper()
        if host:
            statuses.setdefault(host, set()).add(str(check.get("status") or ""))
    verdicts = {}
    for host, seen in statuses.items():
        if "FAIL" in seen:
            verdicts[host] = "unhealthy"
        elif "OK" in seen:
            verdicts[host] = "healthy"
        else:
            verdicts[host] = "unknown"
    return verdicts


async def apply_health(
    app: t.Any, session_id: str, output: str, exit_code: int
) -> dict[str, t.Any] | None:
    """Overlay structured or fallback range health after ``/health``."""
    db = app.state.db
    rng = await db.get_range(session_id)
    if rng is None:
        return None
    report = parse_health_report(output)
    if report is not None:
        per_host = host_health_from_report(report.get("checks") or [])
        for host in rng.get("hosts", []):
            # Reports use config roles (DC01), while variants can rename both
            # the host id and hostname. Older ranges may not yet have ``key``.
            verdict = (
                per_host.get(str(host.get("key") or "").upper())
                or per_host.get(str(host.get("id") or "").upper())
                or per_host.get(str(host.get("hostname") or "").upper())
            )
            if verdict is not None:
                host["health"] = verdict
    else:
        verdict = "healthy" if exit_code == 0 else "unhealthy"
        for host in rng.get("hosts", []):
            if host.get("source") == "config":
                host["health"] = verdict
    await db.upsert_range(session_id, rng)
    return report
