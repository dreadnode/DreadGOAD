"""Condense CLI output into the agent's tool result (design §5.1).

``run_dreadgoad`` hands command output back to the model, and that text is
re-sent with every subsequent turn — so it has to be bounded. A blind tail is
cheap but *lies*: it drops leading records with no marker, and the model then
summarizes the fragment as though it were the whole thing (a 7-VM range read as
5, because the first two scrolled past the cut).

Two strategies, in order of preference:

  - **Structured** — the JSON reads (``/instances``, ``/health``) are already
    parsed elsewhere for the UI overlays, so re-render them as compact lines.
    Complete *and* smaller than the raw output: a 7-VM range goes from ~2.1KB of
    pretty-printed JSON to ~0.4KB, with nothing dropped.
  - **Clipped** — everything else (terraform/ansible logs, human tables) gets a
    middle-out clip that keeps the head *and* tail and states how many lines went
    missing, so the model can see its view is partial and say so.

All functions here are pure and unit-tested; the live wiring is in ``agent.py``.
"""

from __future__ import annotations

import json
import os
import re
import signal
import typing as t

from .hook import parse_health_report

# ``validate`` writes its full results to a JSON file and prints the path. The
# line itself is plain (the surrounding table is not), so match it directly.
# The basename is pinned so a stray path in the output can't make us read an
# arbitrary file off disk.
_VALIDATE_PATH_RE = re.compile(r"Results saved to:\s*(\S+)")
_VALIDATE_BASENAME_RE = re.compile(r"^goad-validation-[\w-]+\.json$")

# CSI sequences (colour, cursor moves). The CLI colourizes freely — `validate`
# draws a boxed table — and neither the chat pane nor the model renders a
# terminal, so raw escapes are literal "[38;2;202;94;68m" noise either way.
_ANSI_RE = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")


def strip_ansi(text: str) -> str:
    """Remove ANSI escape sequences from CLI output bound for a human or a model."""
    return _ANSI_RE.sub("", text)


def describe_exit(exit_code: int) -> str:
    """Phrase a process exit status for the agent's tool result.

    A negative code is POSIX for "killed by signal -N", and in this app that is
    always us: SIGINT from an operator cancel, SIGTERM/SIGKILL from teardown.
    Reporting those as ``failed (exit -2)`` made the model treat a deliberate
    cancel as an error and try to explain output that had simply been cut off
    mid-run — so say plainly that the run stopped early and is incomplete.
    """
    if exit_code == 0:
        return "succeeded"
    if exit_code < 0:
        sig = -exit_code
        if sig == signal.SIGINT:
            return "was cancelled — it stopped early, so this output is incomplete"
        try:
            name = signal.Signals(sig).name
        except ValueError:
            name = f"signal {sig}"
        return f"was terminated ({name}) — this output is incomplete"
    return f"failed (exit {exit_code})"


# Character budget for a clipped tool result. Generous enough to carry a real
# terraform/ansible failure (the error and the context around it), small enough
# that a long op doesn't crowd the conversation it stays resident in.
DEFAULT_LIMIT = 8000

# Per-check lines listed for /health. Failures are what the operator acts on;
# passing checks are counted, not enumerated.
_MAX_HEALTH_LINES = 40


def parse_json_array(output: str) -> list[dict[str, t.Any]] | None:
    """Parse the JSON array emitted by a ``--json`` read command.

    Output is pretty-printed (Go's MarshalIndent) and may be wrapped in stray
    log lines, so slice the ``[`` … ``]`` span and decode. Returns the list (may
    be empty), or None if no JSON array is present.
    """
    start, end = output.find("["), output.rfind("]")
    if start < 0 or end < start:
        return None
    try:
        data = json.loads(output[start : end + 1])
    except (ValueError, TypeError):
        return None
    if not isinstance(data, list):
        return None
    # Elements must be objects too: callers do ``inst.get(...)`` on each, so a
    # bare array of scalars (a stray log line that happens to parse, or a future
    # CLI shape change) would raise inside the agent's tool rather than fall
    # back to the raw output.
    if not all(isinstance(item, dict) for item in data):
        return None
    return data


def clip(text: str, limit: int = DEFAULT_LIMIT) -> str:
    """Middle-out clip to ``limit`` chars, keeping whole lines and saying what fell out.

    Head and tail are both kept — a failing command usually puts the error at the
    end and the invocation context at the start. The marker is the point: an
    unmarked cut is indistinguishable from complete output.
    """
    if len(text) <= limit:
        return text

    lines = text.splitlines()
    half = max(limit // 2, 1)

    def take(seq: list[str]) -> int:
        """How many lines from ``seq`` fit in ``half`` chars."""
        used = 0
        for i, line in enumerate(seq):
            used += len(line) + 1
            if used > half:
                return i
        return len(seq)

    n_head = take(lines)
    n_tail = take(list(reversed(lines)))
    # Don't let the two halves overlap on short-but-wide output.
    if n_head + n_tail > len(lines):
        n_tail = max(len(lines) - n_head, 0)

    if n_head == 0 and n_tail == 0:
        # No line boundary fits the budget — one giant line (minified JSON, a
        # progress bar with no newlines). Cut on characters instead; keeping
        # nothing at all would be a worse lie than the tail we're replacing.
        return (
            f"{text[:half]}\n"
            f"… [{len(text) - 2 * half} characters omitted from the middle] …\n"
            f"{text[-half:]}"
        )

    omitted = len(lines) - n_head - n_tail
    if omitted <= 0:
        return text

    parts = []
    if n_head:
        parts.append("\n".join(lines[:n_head]))
    parts.append(f"… [{omitted} lines omitted from the middle] …")
    if n_tail:
        parts.append("\n".join(lines[len(lines) - n_tail :]))
    return "\n".join(parts)


def summarize_instances(instances: list[dict[str, t.Any]]) -> str:
    """Render ``lab status --json`` as one compact line per instance.

    Keeps every record — the whole point — and drops the cloud resource ids,
    which are ~120 chars each on Azure and which the agent never needs (the
    attack box is discovered by the ingestion hook, not by the model).
    """
    if not instances:
        return "0 instances found (the range is not deployed, or was destroyed)."

    running = sum(1 for i in instances if str(i.get("state", "")).lower() == "running")
    width = max(len(str(i.get("name") or "")) for i in instances)
    lines = [f"{len(instances)} instances ({running} running):"]
    for inst in instances:
        name = str(inst.get("name") or "?")
        state = str(inst.get("state") or "?")
        ip = str(inst.get("private_ip") or "-")
        lines.append(f"  {state:<10} {name:<{width}}  {ip}")
    lines.append("(cloud resource ids omitted)")
    return "\n".join(lines)


def parse_validate_report(output: str) -> dict[str, t.Any] | None:
    """Load the JSON report ``validate`` wrote, given its console output.

    ``validate`` prints a coloured table and saves the machine-readable results
    to ``/tmp/goad-validation-<stamp>.json``; only that path line is ANSI-free,
    so it's what we key on. Returns the parsed report, or None if the line is
    absent, the name doesn't look like a validation report, or the file can't be
    read (it lives on the machine that ran the CLI — always local for us).
    """
    match = _VALIDATE_PATH_RE.search(output)
    if match is None:
        return None
    path = match.group(1)
    if not _VALIDATE_BASENAME_RE.match(os.path.basename(path)):
        return None
    try:
        with open(path, encoding="utf-8") as fh:
            report = json.load(fh)
    except (OSError, ValueError):
        return None
    return report if isinstance(report, dict) and "checks" in report else None


def validate_categories(checks: list[dict[str, t.Any]]) -> list[dict[str, t.Any]]:
    """Roll per-check results up per category, worst-status-first (pure).

    Mirrors the CLI's own summary grid: a category is ``failed`` if any check
    failed, ``partial`` if some were skipped or informational with none passing,
    else ``passed``. ``INFO``/``SKIP`` are "not configured for this variant"
    rather than problems, so they never count as failures.
    """
    order: list[str] = []
    buckets: dict[str, dict[str, int]] = {}
    for check in checks:
        category = str(check.get("category") or "other")
        if category not in buckets:
            buckets[category] = {"passed": 0, "failed": 0, "other": 0}
            order.append(category)
        status = str(check.get("status") or "").upper()
        key = (
            "passed" if status == "PASS" else "failed" if status == "FAIL" else "other"
        )
        buckets[category][key] += 1

    rows: list[dict[str, t.Any]] = []
    for category in order:
        b = buckets[category]
        total = b["passed"] + b["failed"] + b["other"]
        if b["failed"]:
            state = "failed"
        elif b["passed"]:
            state = "passed"
        else:
            state = "skipped"  # nothing asserted — not configured for this variant
        rows.append(
            {
                "category": category,
                "state": state,
                "passed": b["passed"],
                "failed": b["failed"],
                "total": total,
            }
        )
    # Failures first, then categories that asserted something, then the rest.
    rank = {"failed": 0, "passed": 1, "skipped": 2}
    rows.sort(key=lambda r: (rank[r["state"]], r["category"]))
    return rows


def summarize_validate(report: dict[str, t.Any]) -> str:
    """Render a validate report as counts, failures, and the category rollup."""
    checks = report.get("checks") or []
    lines = [
        f"validate: {report.get('passed', 0)} passed, "
        f"{report.get('failed', 0)} failed, {report.get('warnings', 0)} warnings "
        f"({report.get('total_checks', len(checks))} checks)"
    ]
    failures = [c for c in checks if str(c.get("status", "")).upper() == "FAIL"]
    if failures:
        lines.append("failed:")
        for c in failures[:_MAX_HEALTH_LINES]:
            lines.append(f"  {c.get('category', '?')}: {c.get('name', '')}")
        if len(failures) > _MAX_HEALTH_LINES:
            lines.append(f"  … and {len(failures) - _MAX_HEALTH_LINES} more")
    else:
        lines.append("no failed checks.")
    rolled = [r for r in validate_categories(checks) if r["state"] != "passed"]
    if rolled:
        lines.append(
            "not fully configured: "
            + ", ".join(f"{r['category']} {r['passed']}/{r['total']}" for r in rolled)
        )
    return "\n".join(lines)


# `score reset` prints a per-host block; there is no --json verb, so parse the
# shape it does emit. The env banner and the completion line share the
# `=== … ===` form, so they're excluded by name rather than by position.
_SCRUB_HOST_RE = re.compile(r"^=== (?P<host>.+?) ===$")
_SCRUB_MODE_RE = re.compile(r"\bmode=(?P<mode>\S+)")
_SCRUB_TOTAL_RE = re.compile(
    r"Total:\s*(?P<found>\d+) issues? found,\s*(?P<removed>\d+) (?:would remove|removed)"
)
_SCRUB_CLEAN = "clean (no artifacts)"
_SCRUB_SKIP_HOSTS = ("score reset complete",)


def parse_scrub_report(output: str) -> dict[str, t.Any] | None:
    """Parse ``score reset`` console output into per-host cleanup results.

    Returns ``{"mode", "hosts": [{host, found, removed, clean, errors}]}``, or
    None when no host block is present (the command failed early, or the output
    isn't a scrub at all). ANSI is stripped first — the summary lines are
    colourised, so the markers don't match otherwise.
    """
    text = strip_ansi(output)
    mode_match = _SCRUB_MODE_RE.search(text)
    hosts: list[dict[str, t.Any]] = []
    current: dict[str, t.Any] | None = None

    for raw in text.splitlines():
        line = raw.strip()
        header = _SCRUB_HOST_RE.match(line)
        if header:
            name = header.group("host").strip()
            # The run banner is "DreadGOAD score reset (env=…)"; skip it and the
            # closing line so neither becomes a phantom host.
            if (
                name.lower().startswith("dreadgoad")
                or name.lower() in _SCRUB_SKIP_HOSTS
            ):
                current = None
                continue
            current = {
                "host": name,
                "found": 0,
                "removed": 0,
                "clean": False,
                "errors": [],
            }
            hosts.append(current)
            continue
        if current is None:
            continue
        if _SCRUB_CLEAN in line:
            current["clean"] = True
            continue
        total = _SCRUB_TOTAL_RE.search(line)
        if total:
            current["found"] = int(total.group("found"))
            current["removed"] = int(total.group("removed"))
            continue
        if line.startswith("ERROR:"):
            current["errors"].append(line[len("ERROR:") :].strip())

    if not hosts:
        return None
    return {
        "mode": mode_match.group("mode") if mode_match else "unknown",
        "hosts": hosts,
    }


def summarize_scrub(report: dict[str, t.Any]) -> str:
    """Render a scrub report as a mode line plus the hosts that had artifacts."""
    hosts = report.get("hosts") or []
    mode = report.get("mode", "unknown")
    found = sum(int(h.get("found", 0)) for h in hosts)
    dirty = [h for h in hosts if h.get("found") or h.get("errors")]
    verb = "removed" if mode == "apply" else "would remove"

    lines = [
        f"scrub ({mode}): {len(hosts)} hosts checked, "
        f"{found} artifact{'' if found == 1 else 's'} found"
    ]
    if not dirty:
        lines.append("all hosts clean.")
        return "\n".join(lines)
    for h in dirty[:_MAX_HEALTH_LINES]:
        lines.append(
            f"  {h['host']}: {h.get('found', 0)} found, {h.get('removed', 0)} {verb}"
        )
        for err in h.get("errors") or []:
            lines.append(f"    ERROR {err}")
    clean = len(hosts) - len(dirty)
    if clean:
        lines.append(f"({clean} clean host{'' if clean == 1 else 's'} not listed)")
    return "\n".join(lines)


def summarize_health(report: dict[str, t.Any]) -> str:
    """Render a ``health-check --json`` report as counts plus the failures.

    Passing checks are counted, not listed: the operator acts on what broke, and
    a full enumeration is what pushed this over the old limit in the first place.
    """
    passed = report.get("passed", 0)
    failed = report.get("failed", 0)
    skipped = report.get("skipped", 0)
    checks = report.get("checks") or []

    lines = [f"health: {passed} passed, {failed} failed, {skipped} skipped"]
    problems = [c for c in checks if str(c.get("status", "")).upper() != "OK"]
    if not problems:
        lines.append("all checks passed.")
        return "\n".join(lines)

    for c in problems[:_MAX_HEALTH_LINES]:
        status = str(c.get("status") or "?")
        host = str(c.get("host") or "-")
        name = str(c.get("name") or "")
        detail = str(c.get("detail") or "")
        lines.append(
            f"  {status:<5} {host:<8} {name}" + (f" — {detail}" if detail else "")
        )
    if len(problems) > _MAX_HEALTH_LINES:
        lines.append(f"  … and {len(problems) - _MAX_HEALTH_LINES} more non-OK checks")
    if passed:
        lines.append(f"({passed} passing checks not listed)")
    return "\n".join(lines)


def summarize(command: str, output: str, limit: int = DEFAULT_LIMIT) -> str:
    """Condense ``output`` for the agent, structured where possible.

    Falls back to a clip whenever the structured path can't be taken — a command
    that failed before emitting JSON still needs its error text carried through.
    ANSI is stripped up front: on the fallback path the model would otherwise
    read a screenful of escape codes as if they were content.
    """
    output = strip_ansi(output)
    if command == "/instances":
        instances = parse_json_array(output)
        if instances is not None:
            return clip(summarize_instances(instances), limit)
    elif command == "/health":
        report = parse_health_report(output)
        if report is not None:
            return clip(summarize_health(report), limit)
    elif command == "/validate":
        report = parse_validate_report(output)
        if report is not None:
            return clip(summarize_validate(report), limit)
    elif command == "/scrub":
        report = parse_scrub_report(output)
        if report is not None:
            return clip(summarize_scrub(report), limit)
    return clip(output, limit)
