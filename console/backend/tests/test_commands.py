"""Tests for the command registry, argv builder, and CLI runner (Phase 3).

Standalone:  python console/backend/tests/test_commands.py
"""

from __future__ import annotations

import asyncio
import pathlib
import shutil
import stat
import sys
import tempfile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import commands  # noqa: E402
from console.backend.cli import run_command, start_command  # noqa: E402

_SESSION = {"anchor": {"config_path": "/x/dreadgoad.yaml", "env": "dev"}}


def _argv(name: str, extra: list[str] | None = None) -> list[str]:
    return commands.build_argv(_SESSION, name, extra, repo_root="/repo")


def test_argv_injects_config_and_env() -> None:
    a = _argv("/up")
    assert a[0].endswith("dreadgoad"), a
    assert a[1:5] == ["--config", "/x/dreadgoad.yaml", "--env", "dev"], a
    assert a[5:] == ["up"], a
    print("PASS test_argv_injects_config_and_env")


def test_argv_multiword_and_flag_verbs() -> None:
    assert _argv("/reset")[5:] == ["lab", "reset"]
    assert _argv("/instances")[5:] == ["lab", "status", "--json"]
    # /scrub carries --apply by default; see test_scrub_applies_by_default.
    assert _argv("/scrub")[5:] == ["score", "reset", "--apply"]
    print("PASS test_argv_multiword_and_flag_verbs")


def test_argv_arg_shaped_commands() -> None:
    # /extensions: list vs provision
    assert _argv("/extensions")[5:] == ["extension", "list"]
    assert _argv("/extensions", ["elk"])[5:] == ["extension", "provision", "elk"]
    # /score: report path + passthrough flag
    assert _argv("/score", ["/tmp/r.jsonl", "--live-verify"])[5:] == [
        "score",
        "--report",
        "/tmp/r.jsonl",
        "--live-verify",
    ]
    # /variant: passthrough
    assert _argv("/variant", ["--name", "v2"])[5:] == [
        "variant",
        "generate",
        "--name",
        "v2",
    ]
    print("PASS test_argv_arg_shaped_commands")


def test_scrub_applies_by_default() -> None:
    """/scrub cleans for real; a dry token previews instead.

    The CLI defaults to a dry run, the console inverts it — a command whose
    whole purpose is cleaning that silently changed nothing was the more
    surprising behaviour.
    """
    assert _argv("/scrub")[5:] == ["score", "reset", "--apply"]

    for token in (
        "dry",
        "dry-run",
        "dryrun",
        "--dry",
        "--dry-run",
        "-n",
        "preview",
        "DRY",
    ):
        got = _argv("/scrub", [token])[5:]
        assert got == ["score", "reset"], (
            f"{token!r} should suppress --apply, got {got}"
        )

    # Unrecognised args pass straight through, and still apply.
    assert _argv("/scrub", ["--purge-ad"])[5:] == [
        "score",
        "reset",
        "--apply",
        "--purge-ad",
    ]
    # A dry token is consumed, not forwarded to the CLI as a bogus arg.
    assert _argv("/scrub", ["dry", "--purge-ad"])[5:] == [
        "score",
        "reset",
        "--purge-ad",
    ]
    # ...and it's recognised after a flag too, not only in leading position.
    assert _argv("/scrub", ["--purge-ad", "dry"])[5:] == [
        "score",
        "reset",
        "--purge-ad",
    ]

    # Regression: a *value* that happens to spell a dry token must not be eaten.
    # The naive filter dropped --apply here and left --report-output dangling,
    # so a scrub the operator asked to run would silently change nothing.
    assert _argv("/scrub", ["--report-output", "dry"])[5:] == [
        "score",
        "reset",
        "--apply",
        "--report-output",
        "dry",
    ]
    assert _argv("/scrub", ["--ssh-user", "dry"])[5:] == [
        "score",
        "reset",
        "--apply",
        "--ssh-user",
        "dry",
    ]
    # A real dry token after a consumed value is still honoured.
    assert _argv("/scrub", ["--report-output", "/tmp/r.jsonl", "dry"])[5:] == [
        "score",
        "reset",
        "--report-output",
        "/tmp/r.jsonl",
    ]
    print("PASS test_scrub_applies_by_default")


def test_registry_flags_and_parsing() -> None:
    assert commands.REGISTRY["/up"].long_running is True
    assert commands.REGISTRY["/instances"].long_running is False
    # --auto-approve is load-bearing; see test_destroy_carries_auto_approve.
    assert commands.REGISTRY["/destroy"].verb == ("infra", "destroy", "--auto-approve")
    assert commands.is_command("/health") and not commands.is_command("hello there")
    assert commands.parse_command("/score /tmp/r.jsonl --live-verify") == (
        "/score",
        ["/tmp/r.jsonl", "--live-verify"],
    )
    try:
        _argv("/nope")
        raise AssertionError("expected KeyError for unknown command")
    except KeyError:
        pass
    print("PASS test_registry_flags_and_parsing")


def test_dispatch_and_agent_commands() -> None:
    # dispatch drives OPERATOR-typed routing: mutating → agent (expand),
    # reads + /destroy → direct (fast, deterministic).
    assert commands.REGISTRY["/up"].dispatch == "agent"
    assert commands.REGISTRY["/variant"].dispatch == "agent"
    assert commands.REGISTRY["/instances"].dispatch == "direct"
    assert commands.REGISTRY["/destroy"].dispatch == "direct"
    # The agent-dispatch (expand-to-prompt) set = the mutating/arg-flexible ones.
    agent_dispatch = {n for n, c in commands.REGISTRY.items() if c.dispatch == "agent"}
    assert agent_dispatch == {
        "/up",
        "/provision",
        "/reset",
        "/variant",
        "/extensions",
        "/score",
        # /exec takes a free-form request ("restart winrm on dc02") that the
        # agent turns into --hosts/--cmd, so it can't be dispatched directly.
        "/exec",
        # /restart needs a hostname pulled out of the operator's phrasing.
        "/restart",
        # /status runs /instances then /health via the agent in one turn.
        "/status",
    }, agent_dispatch
    # The agent's run_dreadgoad may run ANY registered command (reads + actions).
    assert commands.AGENT_RUNNABLE == frozenset(commands.REGISTRY), (
        commands.AGENT_RUNNABLE
    )
    # Derived, not hardcoded: the point is that AGENT_RUNNABLE covers the whole
    # registry, which the equality above already states. A literal count just
    # breaks every time a command is added and teaches you to bump it.
    assert len(commands.AGENT_RUNNABLE) == len(commands.REGISTRY)
    print("PASS test_dispatch_and_agent_commands")


def test_expand_command_prompt() -> None:
    p = commands.expand_command_prompt("/up", ["using", "the", "variant"])
    assert "/up" in p and "run_dreadgoad" in p, p
    assert "using the variant" in p, p
    assert "raw cloud CLI" in p, "must forbid raw cloud CLI"
    # /up ships prompts/up.md → guidance section present.
    assert "## Command-specific guidance" in p, "up.md guidance expected"
    # no-args form is explicit
    assert "(no extra arguments given)" in commands.expand_command_prompt(
        "/provision", []
    )
    print("PASS test_expand_command_prompt")


def test_load_prompt_and_guidance_injection() -> None:
    """Per-command markdown is loaded and injected; missing files → None."""
    # loader: existing stem vs missing stem
    assert commands.load_prompt("system") is not None, "system.md must exist"
    assert commands.load_prompt("does-not-exist") is None
    # Every agent-dispatch command ships a guidance file, injected before the request.
    agent_dispatch = sorted(
        n for n, c in commands.REGISTRY.items() if c.dispatch == "agent"
    )
    for name in agent_dispatch:
        p = commands.expand_command_prompt(name, [])
        assert "## Command-specific guidance" in p, f"{name} missing guidance"
        gi, oi = p.index("## Command-specific guidance"), p.index("Operator's request:")
        assert gi < oi, f"{name}: guidance must precede operator request"
    # Real content from specific files (not hallucinated).
    assert "variant generate" in commands.expand_command_prompt("/variant", [])
    assert "report path" in commands.expand_command_prompt("/score", []).lower()
    assert "extension list" in commands.expand_command_prompt("/extensions", [])
    up_prompt = commands.expand_command_prompt("/up", [])
    assert "pipeline" in up_prompt.lower()
    assert "continues through provisioning" in up_prompt
    assert "dreadgoad infra apply" in up_prompt
    print("PASS test_load_prompt_and_guidance_injection")


def test_system_prompt_covers_the_registry() -> None:
    """Every command the agent may run is described in system.md.

    The agent can invoke the whole registry, so a command the prompt omits is one
    it runs with no idea of the consequences — /scrub was missing while it was
    already able to delete.

    Placeholders are rendered through the REAL renderer (agent._instructions)
    rather than a list maintained here: the failure mode is system.md gaining a
    ``$field`` that agent.py never substitutes, and a hand-kept list in this test
    would drift in exactly the same way and hide it.
    """
    tpl = commands.load_prompt("system")
    assert tpl is not None
    for name in commands.AGENT_RUNNABLE:
        assert name in tpl, f"{name} is agent-runnable but absent from system.md"

    from console.backend import agent  # local: pulls the agent runtime deps

    rendered = agent._instructions(
        {
            "anchor": {"config_path": "/r/dreadgoad.yaml", "env": "dev"},
            "snapshot": {
                "provider": "aws",
                "lab": "ad/GOAD",
                "region": "us-west-2",
                "variant_name": "redteam",
                "vpc_cidr": "10.0.0.0/16",
            },
        }
    )
    assert "$" not in rendered, "system.md has a placeholder agent.py doesn't fill"
    for value in ("us-west-2", "redteam", "10.0.0.0/16"):
        assert value in rendered, f"{value} missing from the rendered prompt"

    # A fresh session has no region/variant yet; the prompt must say so rather
    # than rendering the literal string "None", which reads as a real value.
    sparse = agent._instructions(
        {"anchor": {"config_path": "/r/c.yaml", "env": "dev"}, "snapshot": {}}
    )
    assert "$" not in sparse and "None" not in sparse, sparse
    assert "(not set)" in sparse, sparse

    # Hook-learned fields must NOT be interpolated: instructions render once and
    # are cached, so they would freeze empty for the life of the session.
    for absent in ("$account", "$group", "$attack_box"):
        assert absent not in tpl, f"{absent} is hook-learned and would go stale"
    # /scrub's console default is inverted vs the CLI's, so the prompt has to say
    # so — otherwise the agent treats a bare /scrub as the CLI's dry run.
    assert "APPLIES BY DEFAULT" in rendered, "/scrub's inverted default must be stated"
    print("PASS test_system_prompt_covers_the_registry")


def test_exec_verb_and_json_flag() -> None:
    """/exec always carries --json, and the agent's flags pass through."""
    # The console adds --json so summarize_exec can parse; the agent is told
    # NOT to pass it, so forgetting it must not degrade the output.
    assert _argv("/exec")[5:] == ["exec", "--json"]
    assert _argv("/exec", ["--hosts", "dc02", "--cmd", "Get-Service WinRM"])[5:] == [
        "exec",
        "--json",
        "--hosts",
        "dc02",
        "--cmd",
        "Get-Service WinRM",
    ]
    # It routes to the agent (free-form request -> flags) and takes args.
    assert commands.REGISTRY["/exec"].dispatch == "agent"
    assert commands.REGISTRY["/exec"].takes_args is True
    # /diagnose is gone — it was a broken playbook, replaced by /exec.
    assert "/diagnose" not in commands.REGISTRY
    print("PASS test_exec_verb_and_json_flag")


def test_restart_targets_one_host() -> None:
    """/restart maps to `lab restart-vm <host>` and demands a hostname.

    The capability already existed in the CLI but wasn't in the registry, so the
    agent inspected /stop and /start, found no per-host flag, and told the
    operator a single-host reboot was impossible.
    """
    assert _argv("/restart", ["dc02"])[5:] == ["lab", "restart-vm", "dc02"]
    # Extra args ride along after the hostname.
    assert _argv("/restart", ["dc02", "--force"])[5:] == [
        "lab",
        "restart-vm",
        "dc02",
        "--force",
    ]
    # Bare /restart must say what's missing rather than hand cobra an empty
    # positional and surface its generic arg error.
    try:
        _argv("/restart", [])
    except ValueError as exc:
        assert "hostname" in str(exc), exc
    else:
        raise AssertionError("bare /restart should be refused")

    # Range-wide power commands stay range-wide; /restart is the per-host one.
    assert _argv("/stop")[5:] == ["lab", "stop"]
    assert _argv("/start")[5:] == ["lab", "start"]
    print("PASS test_restart_targets_one_host")


def test_anchor_cannot_be_overridden_by_extra_args() -> None:
    """Trailing scope selectors must be refused in every Cobra spelling.

    cobra's persistent flags are last-wins, so a trailing copy overrides the
    anchor the console injected and points the command at a DIFFERENT range
    than the session. The agent tool accepts args for every registered command,
    regardless of the UI's ``takes_args`` hint, so every command is covered.
    """
    for name in commands.REGISTRY:
        for bad in (
            ["--config", "/evil.yaml"],
            ["--env", "other"],
            ["--config=/evil.yaml"],
            ["--env=other"],
            ["-c", "/evil.yaml"],
            ["-e", "other"],
            ["-e=other"],
            ["-eother"],
            ["--provider", "aws"],
            ["--provider=aws"],
            ["-p", "aws"],
            ["-p=aws"],
            ["-paws"],
            ["--region", "us-west-2"],
            ["--region=us-west-2"],
            ["--deployment", "other"],
            ["-dother"],
            ["--profile", "other"],
            ["--attack-box", "i-0123456789"],
            ["--hosts", "dc02", "--cmd", "x", "--config", "/evil.yaml"],
        ):
            try:
                _argv(name, bad)
            except ValueError as exc:
                assert "retarget" in str(exc), exc
            else:
                raise AssertionError(f"{name} accepted {bad!r} — range escape")

    # Legitimate args that merely start with the same letters still work.
    assert "--configure" in _argv("/exec", ["--configure", "x"])
    assert _argv("/scrub", ["--purge-ad"])[5:] == [
        "score",
        "reset",
        "--apply",
        "--purge-ad",
    ]
    # And the anchor still leads the argv.
    assert _argv("/exec", ["--hosts", "dc02"])[1:5] == [
        "--config",
        "/x/dreadgoad.yaml",
        "--env",
        "dev",
    ]
    print("PASS test_anchor_cannot_be_overridden_by_extra_args")


def test_exec_guidance_states_the_dangerous_parts() -> None:
    """prompts/exec.md must carry the facts that keep /exec from misfiring."""
    p = commands.expand_command_prompt("/exec", ["restart winrm on dc02"])
    assert "## Command-specific guidance" in p
    # The three that cause silent wrong answers or unintended writes.
    assert "4096" in p, "output cap must be stated or the agent reads fragments"
    assert "no dry run" in p.lower(), "irreversibility must be stated"
    assert "untrusted" in p.lower(), "output is attacker-influenced data"
    assert "/restart <host>" in p, "one failed host must get a targeted restart"
    assert "Do not cycle the whole" in p, "must not power-cycle the whole range"
    print("PASS test_exec_guidance_states_the_dangerous_parts")


def test_guidance_fallback_when_file_absent() -> None:
    """With no prompts/<cmd>.md, the generic template stands (no guidance block)."""
    orig = commands.load_prompt
    commands.load_prompt = lambda stem: None  # type: ignore[assignment]
    try:
        p = commands.expand_command_prompt("/up", ["x"])
        assert "## Command-specific guidance" not in p, "no block when file absent"
        assert "run_dreadgoad" in p and "raw cloud CLI" in p, "generic template intact"
    finally:
        commands.load_prompt = orig
    print("PASS test_guidance_fallback_when_file_absent")


def test_resolve_bin_prefers_repo_binary() -> None:
    """The repo's freshly-built cli/dreadgoad wins over PATH (C3)."""
    with tempfile.TemporaryDirectory() as d:
        repo = pathlib.Path(d)
        cli = repo / "cli"
        cli.mkdir()
        binp = cli / "dreadgoad"
        binp.write_text("#!/usr/bin/env bash\n")
        binp.chmod(binp.stat().st_mode | stat.S_IEXEC)
        assert commands.resolve_bin(repo) == str(binp), "must prefer repo binary"

        # No repo binary → falls back to the expected repo path (clear error),
        # unless dreadgoad is on PATH (env-dependent, so only assert the miss case).
        empty = pathlib.Path(d) / "empty"
        empty.mkdir()
        got = commands.resolve_bin(empty)
        assert got in (
            shutil.which("dreadgoad"),
            str(empty / "cli" / "dreadgoad"),
        ), got
    print("PASS test_resolve_bin_prefers_repo_binary")


def test_parse_command_shlex() -> None:
    """Quoted args (paths with spaces) survive tokenization (C5)."""
    assert commands.parse_command('/score "/tmp/my report.jsonl" --live-verify') == (
        "/score",
        ["/tmp/my report.jsonl", "--live-verify"],
    )
    # Unbalanced quotes → graceful fallback to plain split (no crash).
    name, extra = commands.parse_command('/variant --name "v2')
    assert name == "/variant" and extra == ["--name", '"v2'], (name, extra)
    print("PASS test_parse_command_shlex")


async def test_runner_streams_and_returns_rc() -> None:
    """Runner streams lines and reports the exit code (stubbed CLI)."""
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "fakecli.sh"
        stub.write_text("#!/usr/bin/env bash\necho line-one\necho line-two\nexit 3\n")
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        seen: list[str] = []
        rc, out = await run_command([str(stub)], cwd=d, on_line=seen.append)
        assert rc == 3, f"rc={rc}"
        assert seen == ["line-one", "line-two"], seen
        assert "line-one" in out and "line-two" in out, out
        print("PASS test_runner_streams_and_returns_rc")


async def test_surviving_child_does_not_hang_the_stream() -> None:
    """Regression: a child holding stdout open must not block the turn forever.

    `health-check` on Azure leaves an `az network bastion tunnel` running; it
    inherits the CLI's stdout pipe, so EOF never arrives. Streaming keyed on
    pipe EOF hung here indefinitely, holding the per-session lock and wedging
    the whole chat. The stub reproduces it: background a sleeper that inherits
    stdout, print, exit.
    """
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "leaky.sh"
        stub.write_text(
            "#!/usr/bin/env bash\n"
            "sleep 45 &\n"  # inherits stdout and outlives us — the tunnel
            "echo did-work\n"
            "exit 0\n"
        )
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        rc = await start_command([str(stub)], cwd=d)
        seen: list[str] = []

        async def consume() -> None:
            async for line in rc.stream_lines():
                seen.append(line)

        # Generous vs the 0.25s drain, tiny vs the 45s sleeper: only a fix that
        # keys on process exit can finish inside this.
        await asyncio.wait_for(consume(), timeout=10)
        assert "did-work" in seen, seen
        assert rc.returncode == 0, rc.returncode
        print("PASS test_surviving_child_does_not_hang_the_stream")


async def test_chatty_survivor_does_not_stream_forever() -> None:
    """A survivor that keeps *writing* must not keep the turn alive either.

    Bounding only the idle read isn't enough: a child logging on a timer
    satisfies every read, so the exit check has to run on each iteration and the
    post-exit drain needs a hard ceiling.
    """
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "chatty.sh"
        stub.write_text(
            "#!/usr/bin/env bash\n"
            "( while true; do echo tunnel-noise; sleep 0.05; done ) &\n"
            "echo did-work\n"
            "exit 0\n"
        )
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        rc = await start_command([str(stub)], cwd=d)
        seen: list[str] = []

        async def consume() -> None:
            async for line in rc.stream_lines():
                seen.append(line)

        await asyncio.wait_for(consume(), timeout=10)
        assert "did-work" in seen, seen
        # Bounded by the drain budget, not by the child's lifetime.
        assert len(seen) < 200, f"drained unbounded: {len(seen)} lines"
        print("PASS test_chatty_survivor_does_not_stream_forever")


async def test_output_at_exit_is_not_truncated() -> None:
    """Regression: bounding the read must not drop the CLI's own output."""
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "bursty.sh"
        # 500 lines then immediate exit — all of it must survive.
        stub.write_text(
            "#!/usr/bin/env bash\nfor i in $(seq 1 500); do echo line-$i; done\nexit 0\n"
        )
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        rc, out = await run_command([str(stub)], cwd=d)
        assert rc == 0, rc
        lines = out.splitlines()
        assert len(lines) == 500, f"expected 500 lines, got {len(lines)}"
        assert lines[0] == "line-1" and lines[-1] == "line-500", (lines[0], lines[-1])
        print("PASS test_output_at_exit_is_not_truncated")


async def test_cancel_escalates_to_sigkill() -> None:
    """cancel() SIGKILLs a process that ignores SIGINT, after the grace period."""
    with tempfile.TemporaryDirectory() as d:
        stub = pathlib.Path(d) / "ignore_sigint.sh"
        # Ignore SIGINT, then block — only SIGKILL can stop it.
        stub.write_text("#!/usr/bin/env bash\ntrap '' INT\necho started\nsleep 30\n")
        stub.chmod(stub.stat().st_mode | stat.S_IEXEC)

        rc = await start_command([str(stub)], cwd=d)
        rc._KILL_GRACE = 0.4  # shorten the SIGINT→SIGKILL grace for the test
        stream = rc.stream_lines()
        assert await stream.__anext__() == "started"

        rc.cancel()  # SIGINT is ignored → SIGKILL fires after the grace
        async for _ in stream:  # drains until the process is killed
            pass
        assert rc.cancelled is True
        assert rc.returncode != 0, f"expected non-zero (killed), got {rc.returncode}"
        assert rc._kill_task is None, "completed escalation timer was retained"
        print("PASS test_cancel_escalates_to_sigkill")


def test_destroy_carries_auto_approve() -> None:
    """Without it /destroy cannot destroy anything, ever.

    infra destroy's --auto-approve defaults to false (cli/cmd/infra_cmd.go:90),
    and the terragrunt runner turns that into an explicit --no-auto-approve
    (internal/terragrunt/runner.go:82-83) so tofu prompts. A console command has
    no terminal, so the prompt is an immediate EOF: observed live as every unit
    failing with "error asking for approval: EOF" and nothing being deleted.

    Pinned here because the symptom appears only against real cloud state — the
    argv looks perfectly reasonable, and nothing else in the suite would notice
    the flag going missing.
    """
    session = {"anchor": {"config_path": "/c/dreadgoad.yaml", "env": "rt"}}
    argv = commands.build_argv(session, "/destroy", repo_root=".")
    assert argv[-3:] == ["infra", "destroy", "--auto-approve"], argv


def test_no_console_command_can_block_on_a_prompt() -> None:
    """Every registered verb must be runnable without a terminal.

    The console pipes stdout and never attaches a tty, so any CLI verb that
    waits for input hangs the turn or dies on EOF. This is the generalisation of
    the /destroy failure: the console is a non-interactive caller, always.
    """
    for name, command in commands.REGISTRY.items():
        verb = " ".join(command.verb)
        # `infra apply` and `infra destroy` are the two verbs whose approval
        # flag defaults to false; anything reaching them needs it explicitly.
        if "infra destroy" in verb or "infra apply" in verb:
            assert "--auto-approve" in command.verb, (
                f"{name} runs `{verb}` without --auto-approve; tofu will prompt "
                f"and the console has no terminal to answer it"
            )
        # `init` is the CLI's interactive wizard and must never be mapped.
        assert command.verb[0] != "init", f"{name} maps to the interactive wizard"


def test_catalog_exposes_destructive_for_the_confirm_gate() -> None:
    """The UI confirms before a direct destructive command; it needs the flag.

    `destructive` is deliberately NOT `cloud_ops`. /start and /stop touch real
    cloud resources and are entirely reversible — gating on cloud_ops would put
    a confirmation on both and tell the operator they cannot be undone, which is
    false. Irreversibility is the property that earns the prompt.

    Exposed through the catalog because the alternative is hardcoding "/destroy"
    in the component, and then the next destructive command ships ungated.
    """
    catalog = {c["name"]: c for c in commands.command_catalog()}
    assert len(catalog) == len(commands.REGISTRY)
    for name, entry in catalog.items():
        assert "destructive" in entry, f"{name} missing destructive"
        assert entry["destructive"] == commands.REGISTRY[name].destructive, name

    destroy = catalog["/destroy"]
    assert destroy["destructive"] is True and destroy["dispatch"] == "direct", destroy
    assert destroy["detail"], "the confirm renders detail; it cannot be empty"

    # Reversible power commands must NOT be gated, however cloudy they are.
    for name in ("/start", "/stop"):
        assert commands.REGISTRY[name].cloud_ops is True, name
        assert catalog[name]["destructive"] is False, (
            f"{name} is reversible; confirming it would claim otherwise"
        )

    # Anything else that becomes direct + destructive inherits the confirm, and
    # its `detail` becomes the prompt text — so it has to read correctly.
    gated = {
        n for n, e in catalog.items() if e["destructive"] and e["dispatch"] == "direct"
    }
    assert gated == {"/destroy"}, gated


def test_start_stop_take_an_optional_hostname() -> None:
    """No arg is the whole range; a hostname narrows it to one VM.

    `lab start`/`lab stop` and `lab start-vm`/`stop-vm` are different CLI verbs,
    so this is a shape change rather than a passthrough. The bare form must stay
    byte-identical — it is the common case and was the only one before.
    """
    assert _argv("/start")[5:] == ["lab", "start"]
    assert _argv("/stop")[5:] == ["lab", "stop"]
    assert _argv("/start", ["dc01"])[5:] == ["lab", "start-vm", "dc01"]
    assert _argv("/stop", ["srv02"])[5:] == ["lab", "stop-vm", "srv02"]

    # Surplus positionals pass through to cobra's ExactArgs(1), which rejects
    # them with "accepts 1 arg(s), received 2" — clearer than a guard here.
    assert _argv("/stop", ["dc01", "dc02"])[5:] == ["lab", "stop-vm", "dc01", "dc02"]

    # takes_args drives the UI hint; without it the menu implies no argument.
    assert commands.REGISTRY["/start"].takes_args is True
    assert commands.REGISTRY["/stop"].takes_args is True

    # Still reversible, so still not gated by the destructive confirm.
    assert commands.REGISTRY["/start"].destructive is False
    assert commands.REGISTRY["/stop"].destructive is False
    print("PASS test_start_stop_take_an_optional_hostname")


def test_destroy_takes_an_optional_hostname() -> None:
    """No host tears down everything; a host terminates that one VM.

    Both forms need an approval flag, for the same underlying reason and by two
    different mechanisms: `infra destroy` needs --auto-approve or terragrunt
    withholds -auto-approve and tofu prompts (EOF, exit 1); `lab destroy-vm`
    needs --yes or it reads stdin, prints "Aborted." and returns nil — exit 0
    for a VM that still exists. The second is the more dangerous default,
    because a caller trusting the exit code is told it worked.
    """
    assert _argv("/destroy")[5:] == ["infra", "destroy", "--auto-approve"]
    assert _argv("/destroy", ["dc01"])[5:] == [
        "lab",
        "destroy-vm",
        "dc01",
        "--yes",
    ]
    assert commands.REGISTRY["/destroy"].takes_args is True

    # Both forms are irreversible, so the confirm gate must cover both. It keys
    # on the command, not the argument, so this holds by construction — asserted
    # so that stays true if the gate is ever narrowed.
    assert commands.REGISTRY["/destroy"].destructive is True
    assert commands.REGISTRY["/destroy"].dispatch == "direct"


def main() -> None:
    test_argv_injects_config_and_env()
    test_argv_multiword_and_flag_verbs()
    test_argv_arg_shaped_commands()
    test_scrub_applies_by_default()
    test_registry_flags_and_parsing()
    test_dispatch_and_agent_commands()
    test_expand_command_prompt()
    test_load_prompt_and_guidance_injection()
    test_system_prompt_covers_the_registry()
    test_exec_verb_and_json_flag()
    test_restart_targets_one_host()
    test_anchor_cannot_be_overridden_by_extra_args()
    test_exec_guidance_states_the_dangerous_parts()
    test_guidance_fallback_when_file_absent()
    test_resolve_bin_prefers_repo_binary()
    test_parse_command_shlex()
    asyncio.run(test_runner_streams_and_returns_rc())
    asyncio.run(test_surviving_child_does_not_hang_the_stream())
    asyncio.run(test_chatty_survivor_does_not_stream_forever())
    asyncio.run(test_output_at_exit_is_not_truncated())
    test_destroy_carries_auto_approve()
    test_start_stop_take_an_optional_hostname()
    test_destroy_takes_an_optional_hostname()
    test_catalog_exposes_destructive_for_the_confirm_gate()
    test_no_console_command_can_block_on_a_prompt()
    asyncio.run(test_cancel_escalates_to_sigkill())
    print("ALL PASS")


if __name__ == "__main__":
    main()
