"""DreadGOAD CLI orchestration for console chat turns.

Direct slash commands and agent tool calls share this pipeline so process
ownership, streaming, lifecycle status, ingestion, and report overlays behave
identically regardless of how a command was requested.
"""

from __future__ import annotations

import asyncio
import json
import typing as t
from functools import partial

from . import (
    chat_events,
    chat_runtime,
    commands,
    fetch,
    hook,
    paths,
    projectroot,
    summary,
)
from .cli import start_capture, start_command

# Commands that mutate infra via terraform/ansible need a long graceful runway
# before cancellation escalates to SIGKILL.
_SLOW_CANCEL = frozenset({"/up", "/provision", "/reset", "/destroy", "/extensions"})


def parse_instances(output: str) -> list[dict[str, t.Any]] | None:
    """Parse the JSON array emitted by ``/instances``."""
    return summary.parse_json_array(output)


def _health_progress(line: str) -> str | None:
    """Render one health-check NDJSON record as readable live progress."""
    line = line.strip()
    if not line.startswith("{") or '"status"' not in line or '"checks"' in line:
        return None
    try:
        check = json.loads(line)
    except (ValueError, TypeError):
        return None
    if not isinstance(check, dict) or "checks" in check:
        return None
    status = check.get("status", "?")
    name = check.get("name", "")
    detail = check.get("detail", "")
    return f"{status:<5} {name}" + (f" — {detail}" if detail else "")


def _security_progress(line: str) -> str | None:
    """Render one security-check NDJSON record as readable live progress."""
    line = line.strip()
    if not line.startswith("{") or '"status"' not in line or '"checks"' in line:
        return None
    try:
        check = json.loads(line)
    except (ValueError, TypeError):
        return None
    if not isinstance(check, dict) or "checks" in check:
        return None
    status = check.get("status", "?")
    name = check.get("name", "")
    resource = check.get("resource", "")
    severity = check.get("severity", "")
    detail = check.get("detail", "")
    label = f"{name} [{resource}]" if resource else name
    sev = f" ({severity})" if severity else ""
    return f"{status:<5} {label}{sev}" + (f" — {detail}" if detail else "")


def _parse_security_report(output: str) -> dict[str, t.Any] | None:
    """Extract a security report from NDJSON output (same pattern as health)."""
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
    return None


class _Aborted(Exception):
    """A pre-flight failure with separate operator and caller output."""

    def __init__(self, code: int, emit: str, output: str | None = None) -> None:
        super().__init__(emit)
        self.code = code
        self.emit = emit
        self.output = emit if output is None else output


def final_status(name: str, exit_code: int, cancelled: bool) -> str:
    """Return the session lifecycle status after a long-running command."""
    if cancelled:
        return "interrupted"
    if name in ("/health", "/secure"):
        return "running"
    if exit_code:
        return "error"
    if name == "/destroy":
        return "destroyed"
    return "running"


async def _capture_for_turn(
    session_id: str, argv: list[str], cwd: str
) -> tuple[int, str, str]:
    """Run a machine-readable helper as an owned subprocess of this turn."""
    current = chat_runtime.runtime(session_id)
    turn = current.turn
    if turn is not None:
        turn.commands_starting += 1
    try:
        command = await start_capture(argv, cwd)
    finally:
        if turn is not None:
            turn.commands_starting = max(0, turn.commands_starting - 1)

    if turn is not None and turn.cancelled:
        command.cancel()
    current.running.add(command)
    try:
        result = await command.communicate()
    finally:
        current.running.discard(command)

    turn = current.turn
    if command.cancelled or (turn is not None and turn.cancelled):
        raise asyncio.CancelledError
    return result


def _capture_command(session_id: str) -> fetch.Capture:
    """Bind the generic capture callback to one session runtime."""
    return partial(_capture_for_turn, session_id)


# How long the post-cancel range refresh may take before it is abandoned. The
# operator has just asked for this to stop; a refresh that outlives their
# patience defeats the point of running it. A healthy `lab status` returns in
# about three seconds.
_REFRESH_TIMEOUT = 15.0


async def _capture_for_refresh(
    session_id: str, argv: list[str], cwd: str
) -> tuple[int, str, str]:
    """Capture for the post-cancel refresh: bounded, and never self-cancelling.

    ``_capture_for_turn`` is wrong for this in both directions — it kills the
    command when the turn is cancelled, and raises CancelledError on the way
    out, so the refresh would abort before reading anything. This one runs
    despite the cancellation.

    Bounded because ``capture`` is not: it awaits ``communicate()`` with no
    deadline, so a wedged read would hold the cancel open indefinitely — the
    operator would press cancel and wait longer than if they hadn't. On timeout
    the subprocess is killed rather than left behind.

    The handle joins ``running`` so a shutdown, or a second cancel, can reap it.
    """
    command = await start_capture(argv, cwd)
    current = chat_runtime.runtime(session_id)
    current.running.add(command)
    try:
        return await asyncio.wait_for(command.communicate(), _REFRESH_TIMEOUT)
    except BaseException:
        # Any abnormal exit, not just the timeout: the caller bounds the whole
        # refresh too, and that path cancels this one from outside. Either way
        # the subprocess must die with it rather than outlive the turn.
        command.cancel()
        raise
    finally:
        current.running.discard(command)


async def _prepare_extra(
    session: dict[str, t.Any],
    session_id: str,
    name: str,
    extra: list[str],
) -> list[str]:
    """Resolve arguments that require pre-command work."""
    if name != "/score" or not extra:
        return extra
    try:
        rc_fetch, local, message = await fetch.fetch_report(
            session, extra[0], _capture_command(session_id)
        )
    except ValueError as exc:
        raise _Aborted(1, str(exc)) from exc
    if rc_fetch != 0:
        raise _Aborted(rc_fetch, f"report fetch failed: {message[-300:]}", message)
    return [local, *extra[1:]]


async def _stream_output(
    app: t.Any, session_id: str, name: str, command: t.Any
) -> None:
    """Relay a process's live tail, filtering machine-readable commands."""
    async for line in command.stream_lines():
        line = summary.strip_ansi(line)
        if name == "/instances":
            continue
        if name == "/health":
            progress = _health_progress(line)
            if progress is None:
                continue
            line = progress
        if name == "/secure":
            progress = _security_progress(line)
            if progress is None:
                continue
            line = progress
        await chat_events.emit_event(
            app, session_id, "command_progress", {"line": line}, persist=False
        )


async def _emit_overlays(
    app: t.Any, session_id: str, name: str, output: str, exit_code: int
) -> None:
    """Apply and announce command-specific range overlays and reports."""
    if name == "/health":
        report = await hook.apply_health(app, session_id, output, exit_code)
        if report is not None:
            await chat_events.emit_event(
                app,
                session_id,
                "health_report",
                {
                    "passed": report.get("passed", 0),
                    "failed": report.get("failed", 0),
                    "skipped": report.get("skipped", 0),
                    "checks": report.get("checks", []),
                },
            )
    elif name == "/instances":
        instances = parse_instances(output)
        if instances is not None:
            running = sum(
                1
                for instance in instances
                if str(instance.get("state", "")).lower() == "running"
            )
            await chat_events.emit_event(
                app,
                session_id,
                "instances_report",
                {
                    "instances": instances,
                    "total": len(instances),
                    "running": running,
                },
            )
    elif name == "/validate":
        report = summary.parse_validate_report(output)
        if report is not None:
            checks = report.get("checks") or []
            await chat_events.emit_event(
                app,
                session_id,
                "validate_report",
                {
                    "passed": report.get("passed", 0),
                    "failed": report.get("failed", 0),
                    "warnings": report.get("warnings", 0),
                    "total": report.get("total_checks", len(checks)),
                    "categories": summary.validate_categories(checks),
                    "failures": [
                        {
                            "category": check.get("category", ""),
                            "name": check.get("name", ""),
                        }
                        for check in checks
                        if str(check.get("status", "")).upper() == "FAIL"
                    ],
                },
            )
    elif name == "/scrub":
        report = summary.parse_scrub_report(output)
        if report is not None:
            hosts = report.get("hosts") or []
            await chat_events.emit_event(
                app,
                session_id,
                "scrub_report",
                {
                    "mode": report.get("mode", "unknown"),
                    "hosts": hosts,
                    "found": sum(int(host.get("found", 0)) for host in hosts),
                    "removed": sum(int(host.get("removed", 0)) for host in hosts),
                },
            )
    elif name == "/exec":
        results = summary.parse_json_array(output)
        if results is not None:
            results = summary.clean_exec_results(results)
            await chat_events.emit_event(
                app,
                session_id,
                "exec_report",
                {
                    "results": results,
                    "succeeded": sum(
                        1
                        for result in results
                        if summary.exec_succeeded(result.get("status"))
                    ),
                    "total": len(results),
                },
            )
    elif name == "/secure":
        report = _parse_security_report(output)
        if report is not None:
            checks = report.get("checks") or []
            await chat_events.emit_event(
                app,
                session_id,
                "security_report",
                {
                    "passed": report.get("passed", 0),
                    "failed": report.get("failed", 0),
                    "warned": report.get("warned", 0),
                    "skipped": report.get("skipped", 0),
                    "security_checks": checks,
                },
            )
    elif name in ("/variant", "/extensions"):
        await hook.reseed(app, session_id, _capture_command(session_id))


async def run_cli(
    app: t.Any, session_id: str, name: str, extra: list[str] | None = None
) -> tuple[int, str]:
    """Run one DreadGOAD command through the shared console pipeline."""
    session = await app.state.db.get_session(session_id)
    if session is None:
        await chat_events.emit_event(
            app, session_id, "error", {"message": "session not found"}
        )
        return 1, "session not found"

    try:
        extra = await _prepare_extra(session, session_id, name, list(extra or []))
    except _Aborted as exc:
        await chat_events.emit_event(app, session_id, "error", {"message": exc.emit})
        return exc.code, exc.output

    try:
        argv = commands.build_argv(
            session, name, extra, repo_root=str(paths.repo_root())
        )
    except ValueError as exc:
        await chat_events.emit_event(app, session_id, "error", {"message": str(exc)})
        return 1, str(exc)

    # Where the CLI will resolve the range's files. The config path and the
    # working directory are independent inputs to the CLI (see projectroot),
    # and this used to be a fixed repo_root() — so a config in another checkout
    # had its inventory and lab data looked up in the console's tree instead of
    # its own. Running in the config's directory makes the CLI's inference land
    # where it would running by hand next to that config.
    #
    # repo_root() still locates the *binary* in build_argv above; that is the
    # console's own checkout and is a separate question from where the range's
    # files live.
    config_path = projectroot.config_path_of(session)
    if config_path:
        # long_running is the registry's marker for the commands that drive
        # hosts (/health, /provision, /reset, /exec, /up, /validate) as opposed
        # to cloud-only reads (/instances). Only those need an inventory, and
        # warning about it on every read would make the warning worth ignoring.
        checks = projectroot.preflight(
            config_path,
            session["anchor"]["env"],
            check_inventory=commands.REGISTRY[name].long_running,
        )
        run_cwd = str(checks.root)
        # Advisory, and emitted before the spawn: a missing inventory otherwise
        # surfaces only as every host failing identically, long afterwards.
        for warning in checks.warnings:
            await chat_events.emit_event(
                app, session_id, "status", {"content": warning}
            )
    else:
        run_cwd = str(paths.repo_root())

    await chat_events.emit_event(
        app,
        session_id,
        "command_run",
        {"phase": "start", "command": name, "argv": argv, "cwd": run_cwd},
    )

    current = chat_runtime.runtime(session_id)
    turn = current.turn
    if turn is not None:
        turn.command = name

    command_spec = commands.REGISTRY[name]
    if command_spec.long_running:
        await app.state.sessions.set_status(session_id, "provisioning")

    if turn is not None:
        turn.commands_starting += 1
    try:
        command = await start_command(argv, cwd=run_cwd)
    except OSError as exc:
        message = f"failed to start {name}: {exc}"
        if command_spec.long_running:
            await app.state.sessions.set_status(session_id, "error")
        await chat_events.emit_event(app, session_id, "error", {"message": message})
        await chat_events.emit_event(
            app,
            session_id,
            "command_run",
            {
                "phase": "end",
                "command": name,
                "exit_code": 1,
                "cancelled": False,
                "tail": message,
            },
        )
        return 1, message
    finally:
        if turn is not None:
            turn.commands_starting = max(0, turn.commands_starting - 1)

    command._KILL_GRACE = 300.0 if name in _SLOW_CANCEL else 12.0
    if turn is not None and turn.cancelled:
        command.cancel()
    current.running.add(command)
    try:
        await _stream_output(app, session_id, name, command)
    finally:
        current.running.discard(command)
    exit_code, output = command.returncode, command.output

    if command_spec.long_running:
        await app.state.sessions.set_status(
            session_id, final_status(name, exit_code, command.cancelled)
        )

    cancelled = command.cancelled or (
        current.turn is not None and current.turn.cancelled
    )
    await chat_events.emit_event(
        app,
        session_id,
        "command_run",
        {
            "phase": "end",
            "command": name,
            "exit_code": exit_code,
            "cancelled": command.cancelled,
            # Cancelling kills our subprocess; it does not reach into Azure or
            # into a playbook already running on a host. Saying only "cancelled"
            # let an operator watch a DC reboot they believed they had stopped.
            "still_running": bool(cancelled and command_spec.cloud_ops),
            "tail": output[-2000:],
        },
    )

    # Re-check after the emit, not before. emit_event awaits a store write and a
    # socket send, so a cancel can land inside it; deciding from the pre-emit
    # value would let that turn carry on as if nothing had happened. The payload
    # above keeps the value that was true when it was sent.
    cancelled = cancelled or (current.turn is not None and current.turn.cancelled)

    if cancelled:
        # Re-read the range before unwinding. The command may have changed the
        # world on its way out, and the view would otherwise keep showing the
        # state from before it ran — the one moment the display is most likely
        # to be wrong is the one we were skipping.
        #
        # Deliberately NOT the turn-owned capture: that raises CancelledError
        # the moment it sees turn.cancelled, so the refresh would abort before
        # doing anything. Failure here must not replace the cancellation, so
        # everything is swallowed except the cancel itself.
        if command_spec.cloud_ops:
            # Bounded as a whole, not just at the subprocess: the operator has
            # already asked for this to stop, so a refresh that outlives their
            # patience makes cancelling slower than not cancelling. Anything
            # inside run_check can block — a wedged read, a slow store — and
            # only a deadline around all of it is actually a guarantee.
            try:
                payload = await asyncio.wait_for(
                    hook.run_check(
                        app, session_id, partial(_capture_for_refresh, session_id)
                    ),
                    _REFRESH_TIMEOUT,
                )
                await chat_events.emit_event(app, session_id, "check_run", payload)
            except asyncio.CancelledError:
                raise
            except Exception:  # noqa: BLE001 - a stale view beats a lost cancel
                pass
        raise asyncio.CancelledError

    payload = await hook.run_check(app, session_id, _capture_command(session_id))
    await chat_events.emit_event(app, session_id, "check_run", payload)
    await _emit_overlays(app, session_id, name, output, exit_code)
    return exit_code, output
