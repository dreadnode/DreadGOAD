"""Live TUI status board using Rich."""

import json
import time
from dataclasses import dataclass
from datetime import datetime, timezone

from rich import box
from rich.console import Console, Group
from rich.live import Live
from rich.panel import Panel
from rich.table import Table
from rich.text import Text

from .verify import StatusReport, verify_report, parse_report

# Dreadnode color palette
C_SUCCESS = "#68c147"
C_ERROR = "#e44f4f"
C_WARNING = "#c8ac4a"
C_INFO = "#4689bf"
C_BRAND = "#ca5e44"
C_ACCENT = "#ef562f"
C_PURPLE = "#a650fb"
C_TEAL = "#20dfc8"
C_FG = "#e2e7ec"
C_FG_SUBTLE = "#c1c6cc"
C_FG_MUTED = "#9da0a5"
C_FG_FAINTEST = "#686d73"
C_BORDER = "#2b343f"

# Group display config
GROUP_CONFIG = {
    "credentials": {
        "title": "CREDENTIALS DISCOVERED",
        "short": "CREDENTIALS",
        "color": f"bold {C_BRAND}",
    },
    "hosts": {
        "title": "HOSTS COMPROMISED",
        "short": "HOSTS",
        "color": f"bold {C_BRAND}",
    },
    "domains": {
        "title": "DOMAINS OWNED",
        "short": "DOMAINS",
        "color": f"bold {C_BRAND}",
    },
    "techniques": {
        "title": "ATTACK TECHNIQUES USED",
        "short": "ATTACK TECHNIQUES",
        "color": f"bold {C_BRAND}",
    },
}

# Layout: left column groups, right column groups
LEFT_GROUPS = ["domains", "hosts", "techniques"]
RIGHT_GROUPS = ["credentials"]


@dataclass
class PollState:
    """Tracks polling status for the footer bar."""

    last_poll_time: float = 0.0
    poll_interval: float = 3.0
    last_result: str = "waiting"  # "ok", "no_file", "error", "waiting"
    last_error: str = ""
    finding_count: int = 0
    report_path: str = "/tmp/report.jsonl"


def build_header(status: StatusReport, agent_id: str, elapsed: str) -> Table:
    """Build the header bar with colorful stats."""
    table = Table(show_header=False, show_edge=False, pad_edge=False, expand=True)
    table.add_column(ratio=1)
    table.add_column(ratio=1, justify="right")

    summary = Text()
    first = True
    for group, stats in status.groups.items():
        cfg = GROUP_CONFIG.get(group, {"title": group.upper(), "color": "white"})
        label = cfg.get("short", cfg["title"])
        color = cfg["color"]

        if not first:
            summary.append("  |  ", style=C_FG_FAINTEST)
        summary.append(f"{label} ", style=color)
        achieved = stats["achieved"]
        total = stats["total"]
        summary.append(f"{achieved}", style=f"bold {C_SUCCESS}")
        summary.append("/", style=C_FG)
        summary.append(f"{total}", style=C_INFO)
        first = False

    table.add_row(summary, Text(f"Agent: {agent_id}  |  {elapsed}", style=C_FG_MUTED))
    return table


def build_group_section(
    group: str, stats: dict, verified: list, answer_key: dict
) -> Table:
    """Build a section for one milestone group."""
    cfg = GROUP_CONFIG.get(group, {"title": group.upper(), "color": "bold white"})
    achieved = stats["achieved"]
    total = stats["total"]

    table = Table(
        show_header=False,
        show_edge=False,
        pad_edge=True,
        title=f"  {cfg['title']}  ({achieved}/{total})",
        title_style=cfg["color"],
        title_justify="left",
        expand=True,
        box=box.SIMPLE,
        padding=(0, 1, 0, 0),
    )
    table.add_column("status", width=4, no_wrap=True)
    table.add_column("label", ratio=1)
    table.add_column("time", width=10, justify="right", no_wrap=True)

    achieved_ids = {}
    for vo in verified:
        if vo.group == group and vo.verified:
            achieved_ids[vo.objective_id] = vo

    group_objectives = [
        o for o in answer_key.get("objectives", []) if o["group"] == group
    ]

    for obj in group_objectives:
        vo = achieved_ids.get(obj["id"])
        if vo:
            ts = _format_ts(vo.timestamp)
            table.add_row(
                Text("[x]", style=f"bold {C_SUCCESS}"),
                Text(obj["label"]),
                Text(ts, style=C_FG_MUTED),
            )
        else:
            hint = obj.get("hint", "") or ""
            label_text = obj["label"]
            if hint:
                label_text += f"  ({hint})"
            table.add_row(
                Text("[ ]", style=C_FG_FAINTEST),
                Text(label_text, style=C_FG_FAINTEST),
                Text(""),
            )

    return table


def _format_ts(timestamp: str) -> str:
    if not timestamp:
        return ""
    try:
        dt = datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
        return dt.strftime("%H:%M:%S")
    except ValueError:
        return timestamp[:8]


def build_poll_footer(poll: PollState) -> Text:
    """Build the polling status footer line."""
    now = time.monotonic()
    since_poll = now - poll.last_poll_time
    next_in = max(0, poll.poll_interval - since_poll)

    footer = Text()

    # Status indicator
    if poll.last_result == "ok":
        footer.append("  CONNECTED", style=f"bold {C_SUCCESS}")
        footer.append(f"  ({poll.finding_count} findings)", style=C_FG_MUTED)
    elif poll.last_result == "no_file":
        footer.append("  WAITING FOR REPORT", style=f"bold {C_WARNING}")
        footer.append(f"  ({poll.report_path})", style=C_FG_FAINTEST)
    elif poll.last_result == "error":
        footer.append("  FETCH ERROR", style=f"bold {C_ERROR}")
        if poll.last_error:
            footer.append(f"  ({poll.last_error})", style=C_FG_MUTED)
    else:
        footer.append("  CONNECTING...", style=f"bold {C_INFO}")

    # Countdown
    footer.append(f"  |  next poll: {next_in:.0f}s", style=C_FG_FAINTEST)

    return footer


def build_status_board(
    status: StatusReport,
    agent_id: str,
    start_time: datetime | None,
    answer_key: dict,
    poll: PollState | None = None,
) -> Panel:
    """Build the full status board panel with two-column layout."""
    if start_time:
        elapsed = str(
            datetime.now(timezone.utc).replace(tzinfo=None) - start_time
        ).split(".")[0]
    else:
        elapsed = "--:--:--"

    header = build_header(status, agent_id, elapsed)

    # Build left column sections
    left_sections = []
    for group in LEFT_GROUPS:
        stats = status.groups.get(group)
        if not stats or stats["total"] == 0:
            continue
        left_sections.append(
            build_group_section(group, stats, status.verified, answer_key)
        )
        left_sections.append(Text(""))

    # Build right column sections
    right_sections = []
    for group in RIGHT_GROUPS:
        stats = status.groups.get(group)
        if not stats or stats["total"] == 0:
            continue
        right_sections.append(
            build_group_section(group, stats, status.verified, answer_key)
        )
        right_sections.append(Text(""))

    left_col = Group(*left_sections) if left_sections else Text("")
    right_col = Group(*right_sections) if right_sections else Text("")

    columns = Table(
        show_header=False,
        show_edge=False,
        pad_edge=False,
        expand=True,
        border_style=C_BORDER,
        show_lines=False,
    )
    columns.add_column(ratio=1, vertical="top")
    columns.add_column(ratio=1, vertical="top")
    columns.add_row(left_col, right_col)

    # Footer
    footer_parts = []
    if status.unmatched_findings:
        footer_parts.append(
            Text(
                f"  + {len(status.unmatched_findings)} additional finding(s) reported",
                style=f"italic {C_FG_FAINTEST}",
            )
        )
    if poll:
        footer_parts.append(build_poll_footer(poll))

    content = Group(header, Text(""), columns, *footer_parts)

    return Panel(
        content,
        title=f"[bold {C_BRAND}]DreadGOAD STATUS BOARD[/bold {C_BRAND}]",
        border_style=C_BRAND,
        expand=True,
    )


def run_tui(
    transport,
    answer_key: dict,
    poll_interval: float = 3.0,
    report_path: str = "/tmp/report.jsonl",
):
    """Main TUI loop. Polls transport for report updates and refreshes display."""
    console = Console()
    agent_id = "dreadnode-agent"
    start_time = None
    last_report_hash = None

    empty_report = {"agent_id": "dreadnode-agent", "findings": []}
    status = verify_report(empty_report, answer_key)
    poll = PollState(poll_interval=poll_interval, report_path=report_path)

    console.print(
        f"[bold {C_BRAND}]DreadGOAD Status Board[/bold {C_BRAND}] starting..."
    )
    console.print(f"Polling every {poll_interval}s. Press Ctrl+C to exit.\n")

    with Live(
        build_status_board(status, agent_id, start_time, answer_key, poll),
        console=console,
        refresh_per_second=2,
    ) as live:
        while True:
            try:
                # Poll for report
                try:
                    raw = transport.fetch_report()
                    poll.last_error = ""
                except Exception as e:
                    raw = None
                    poll.last_result = "error"
                    poll.last_error = str(e)
                poll.last_poll_time = time.monotonic()

                if raw:
                    poll.last_result = "ok"
                    poll.last_error = ""
                    report_hash = hash(raw)
                    if report_hash != last_report_hash:
                        last_report_hash = report_hash
                        report = parse_report(raw)
                        agent_id = report.get("agent_id", "dreadnode-agent")
                        poll.finding_count = len(report.get("findings", []))
                        if report.get("start_time") and not start_time:
                            try:
                                start_time = datetime.fromisoformat(
                                    report["start_time"].replace("Z", "+00:00")
                                ).replace(tzinfo=None)
                            except ValueError:
                                pass
                        status = verify_report(report, answer_key)
                elif poll.last_result != "error":
                    poll.last_result = "no_file"

                # Update display at higher rate for countdown
                for _ in range(int(poll_interval * 2)):
                    live.update(
                        build_status_board(
                            status, agent_id, start_time, answer_key, poll
                        )
                    )
                    time.sleep(0.5)

            except KeyboardInterrupt:
                break
            except json.JSONDecodeError:
                poll.last_result = "error"
                time.sleep(poll_interval)
                continue

    console.print(f"\n[bold {C_FG}]Final status:[/bold {C_FG}]")
    console.print(build_status_board(status, agent_id, start_time, answer_key, poll))
