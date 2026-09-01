"""Tests for agent tool-result condensing (summary.py).

Standalone:  python console/backend/tests/test_summary.py
"""

from __future__ import annotations

import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from console.backend import command_runner, summary  # noqa: E402


def _instances(n: int) -> list[dict[str, str]]:
    return [
        {
            "name": f"dreadindex-dreadgoad-vm{i:02d}",
            # Azure resource ids are ~120 chars — the bulk of the raw output.
            "id": f"/subscriptions/70a9c8a4-6bc6-4a48-ae24-27996cea8c02/resourceGroups/"
            f"DREADINDEX-DREADGOAD-RG/providers/Microsoft.Compute/virtualMachines/vm{i:02d}",
            "state": "running",
            "private_ip": f"10.1.1.{i}",
        }
        for i in range(n)
    ]


def test_regression_seven_vms_all_survive() -> None:
    """The reported bug: a 7-VM range read as 5 under the old 1500-char tail."""
    raw = json.dumps(_instances(7), indent=2)
    assert len(raw) > 1500, "fixture must exceed the old cap to be a regression test"
    assert raw[-1500:].count('"name"') == 5, "fixture must reproduce the old undercount"

    out = summary.summarize("/instances", raw)
    for i in range(7):
        assert f"vm{i:02d}" in out, f"vm{i:02d} dropped from the summary"
    assert out.startswith("7 instances (7 running):"), out
    assert len(out) < len(raw), "summary must be smaller than the raw output"
    print("PASS test_regression_seven_vms_all_survive")


def test_instances_scale_and_states() -> None:
    """A 60-VM range still fits, and mixed power states are counted correctly."""
    insts = _instances(60)
    for i in range(0, 60, 3):
        insts[i]["state"] = "deallocated"
    out = summary.summarize("/instances", json.dumps(insts, indent=2))
    assert out.startswith("60 instances (40 running):"), out.splitlines()[0]
    assert out.count("deallocated") == 20, out
    assert len(out) <= summary.DEFAULT_LIMIT, len(out)
    print("PASS test_instances_scale_and_states")


def test_instances_surface_cloud_placement() -> None:
    """Account/resource group reach the agent — system.md promises this read has them.

    They're identical across a range, so they're reported once, not per row.
    """
    azure = _instances(3)
    for i in azure:
        i["account"] = "70a9c8a4-6bc6-4a48-ae24-27996cea8c02"
        i["group"] = "DREADINDEX-DREADGOAD-RG"
    out = summary.summarize("/instances", json.dumps(azure))
    assert "70a9c8a4-6bc6-4a48-ae24-27996cea8c02" in out, out
    assert "DREADINDEX-DREADGOAD-RG" in out, out
    assert out.count("DREADINDEX-DREADGOAD-RG") == 1, "once, not per instance"

    # AWS: an account but no resource group — no empty "resource group" field.
    aws = _instances(2)
    for i in aws:
        i["account"] = "123456789012"
    out = summary.summarize("/instances", json.dumps(aws))
    assert "123456789012" in out, out
    assert "resource group" not in out, out

    # Pre-deploy / older CLI: neither key present → no placement line at all.
    out = summary.summarize("/instances", json.dumps(_instances(2)))
    assert "deployed into" not in out, out
    print("PASS test_instances_surface_cloud_placement")


def test_summarize_exec_per_host_blocks() -> None:
    """/exec renders status + output per host, mixed outcomes included."""
    out = summary.summarize(
        "/exec",
        json.dumps(
            [
                {
                    "host": "env-DC02-vm",
                    "instance_id": "/s/1",
                    "status": "Succeeded",
                    "stdout": "Status : Stopped\nStartType : Manual",
                    "stderr": "",
                },
                {
                    "host": "env-DC03-vm",
                    "instance_id": "/s/2",
                    "status": "Failed",
                    "stdout": "",
                    "stderr": "run command timed out",
                },
            ]
        ),
    )
    assert "2 host(s), 1 succeeded" in out, out
    assert "env-DC02-vm" in out and "StartType : Manual" in out, out
    assert "STDERR: run command timed out" in out, out
    print("PASS test_summarize_exec_per_host_blocks")


def test_summarize_exec_flags_truncated_output() -> None:
    """Azure silently caps a stream at 4096 bytes — say so, don't let the agent
    treat the fragment as the whole answer."""
    capped = "A" * 4096
    out = summary.summarize(
        "/exec",
        json.dumps(
            [
                {
                    "host": "h",
                    "instance_id": "i",
                    "status": "Succeeded",
                    "stdout": capped,
                    "stderr": "",
                }
            ]
        ),
        limit=99_000,
    )
    assert "almost certainly truncated" in out, out
    # Just under the cap is NOT flagged — a false warning teaches the agent to
    # ignore the real one.
    ok = summary.summarize(
        "/exec",
        json.dumps(
            [
                {
                    "host": "h",
                    "instance_id": "i",
                    "status": "Succeeded",
                    "stdout": "A" * 4095,
                    "stderr": "",
                }
            ]
        ),
        limit=99_000,
    )
    assert "truncated" not in ok, "sub-cap output must not be flagged"

    # Regression: the length was measured AFTER .strip(), so a capped stream
    # ending in whitespace read as 4095 and was silently not flagged — and
    # PowerShell output nearly always ends in a newline, so that was the
    # common case, not an edge case.
    for tail in ("\n", "\r\n", "  ", " \n"):
        padded = "A" * (4096 - len(tail)) + tail
        assert len(padded) == 4096
        out = summary.summarize(
            "/exec",
            json.dumps(
                [
                    {
                        "host": "h",
                        "instance_id": "i",
                        "status": "Succeeded",
                        "stdout": padded,
                        "stderr": "",
                    }
                ]
            ),
            limit=99_000,
        )
        assert "almost certainly truncated" in out, f"tail {tail!r} not flagged"
    print("PASS test_summarize_exec_flags_truncated_output")


def test_exec_decodes_utf16_from_a_stale_cli() -> None:
    """Defence in depth: the CLI normalises UTF-16, but binaries can skew.

    Windows PowerShell writes its fatal banner as UTF-16LE. provider.CleanResult
    handles it Go-side, but the console can be pointed at an older
    cli/dreadgoad than it shipped with, and a raw NUL in a JSON string is
    garbage to both the model and the browser regardless.
    """
    banner = "Windows PowerShell terminated with the following error:"
    utf16 = "".join(c + "\x00" for c in banner)
    out = summary.summarize(
        "/exec",
        json.dumps([{"host": "h", "status": "Failed", "stdout": "", "stderr": utf16}]),
    )
    assert banner in out, out
    assert "\x00" not in out, "NULs reached the model"

    # Ordinary output must be byte-identical — this runs on every result.
    for plain in ("", "Status : Running", "ünïcode ✓ 日本語", "A" * 4096):
        assert summary.decode_windows_text(plain) == plain, repr(plain)
    # A stray NUL in UTF-8 text is dropped, not misread as UTF-16.
    assert summary.decode_windows_text("service\x00 stopped") == "service stopped"
    print("PASS test_exec_decodes_utf16_from_a_stale_cli")


def test_exec_success_vocabulary_matches_the_cli() -> None:
    """The CLI emits "Success"; matching only "Succeeded" hid every success.

    Every provider in the Go tree (azure winrm + runcommand, ludus, proxmox)
    sets Status="Success". Counting only "Succeeded" reported a fully healthy
    run as 0 succeeded — invisible until a host actually worked.
    """
    for ok in ("Success", "success", "SUCCESS", "Succeeded", " succeeded "):
        assert summary.exec_succeeded(ok), ok
    for bad in ("Failed", "Error", "no result", "", None, "Successish"):
        assert not summary.exec_succeeded(bad), bad

    # End to end: a healthy two-host run must read as fully succeeded.
    out = summary.summarize(
        "/exec",
        json.dumps(
            [
                {"host": "a", "status": "Success", "stdout": "ok", "stderr": ""},
                {"host": "b", "status": "Success", "stdout": "ok", "stderr": ""},
            ]
        ),
    )
    assert out.startswith("2 host(s), 2 succeeded:"), out
    print("PASS test_exec_success_vocabulary_matches_the_cli")


def test_summarize_exec_strips_ansi_from_host_output() -> None:
    """Colourised host output must not reach the model or the chat pane.

    summarize()'s up-front strip_ansi can't catch this: inside the JSON the
    escape is the six characters ``\\u001b``, so the CSI pattern doesn't match
    until after json.loads. PowerShell 7 colourises by default, so /exec would
    otherwise hand raw escapes to both consumers.
    """
    payload = json.dumps(
        [
            {
                "host": "h",
                "instance_id": "i",
                "status": "Succeeded",
                "stdout": "\x1b[32;1mStatus\x1b[0m : Stopped",
                "stderr": "\x1b[31mboom\x1b[0m",
            }
        ]
    )
    out = summary.summarize("/exec", payload)
    assert "\x1b" not in out, "agent path still sees escapes"
    assert "Status : Stopped" in out and "STDERR: boom" in out, out

    # The command overlay parses separately and must clean
    # the same way, or the UI renders literal escape codes.
    cleaned = summary.clean_exec_results(summary.parse_json_array(payload) or [])
    assert cleaned[0]["stdout"] == "Status : Stopped", cleaned
    assert cleaned[0]["stderr"] == "boom", cleaned

    # Cleaning must not mutate the caller's list, which the overlay also holds.
    original = [{"host": "h", "stdout": "\x1b[31mx\x1b[0m", "stderr": ""}]
    summary.clean_exec_results(original)
    assert original[0]["stdout"] == "\x1b[31mx\x1b[0m", "input was mutated"
    print("PASS test_summarize_exec_strips_ansi_from_host_output")


def test_summarize_exec_edge_cases() -> None:
    assert "nothing ran" in summary.summarize("/exec", "[]")
    # A host that produced neither stream is stated, not silently blank.
    out = summary.summarize(
        "/exec",
        json.dumps(
            [
                {
                    "host": "h",
                    "instance_id": "i",
                    "status": "Succeeded",
                    "stdout": "",
                    "stderr": "",
                }
            ]
        ),
    )
    assert "(no output)" in out, out
    # Not JSON (the verb failed before emitting any) → error text preserved.
    err = 'host "dc0" is ambiguous, matches: DC01, DC02'
    assert err in summary.summarize("/exec", err)
    print("PASS test_summarize_exec_edge_cases")


def test_instances_empty_and_unparseable() -> None:
    assert "0 instances" in summary.summarize("/instances", "[]")
    # Not JSON at all (e.g. the command errored) → the error text is preserved.
    err = "load variant mapping: no such file or directory"
    assert err in summary.summarize("/instances", err)
    print("PASS test_instances_empty_and_unparseable")


def test_non_object_array_falls_back_instead_of_raising() -> None:
    """A JSON array of scalars must not blow up inside the agent's tool."""
    for bad in ("[1, 2, 3]", "[null]", '["a"]', "[[]]"):
        assert summary.parse_json_array(bad) is None, bad
        # summarize must degrade to the raw output, not raise.
        assert bad in summary.summarize("/instances", bad), bad
    # A well-formed array of objects still parses.
    assert summary.parse_json_array('[{"name": "x"}]') == [{"name": "x"}]
    print("PASS test_non_object_array_falls_back_instead_of_raising")


def test_health_lists_failures_not_passes() -> None:
    checks = [
        {"name": f"chk{i}", "host": "DC01", "status": "OK", "detail": "fine"}
        for i in range(30)
    ]
    checks.append(
        {"name": "smb", "host": "DC02", "status": "FAIL", "detail": "port closed"}
    )
    report = {"passed": 30, "failed": 1, "skipped": 0, "checks": checks}
    out = summary.summarize("/health", json.dumps(report))
    assert out.startswith("health: 30 passed, 1 failed, 0 skipped"), out
    assert "smb" in out and "port closed" in out, "the failure must survive"
    assert "chk7" not in out, "passing checks should not be enumerated"
    assert "(30 passing checks not listed)" in out, out
    print("PASS test_health_lists_failures_not_passes")


def test_health_all_passed_and_prefail() -> None:
    report = {
        "passed": 3,
        "failed": 0,
        "skipped": 0,
        "checks": [{"name": "a", "host": "DC01", "status": "OK", "detail": ""}],
    }
    assert "all checks passed." in summary.summarize("/health", json.dumps(report))
    # Failed before emitting a report → raw error text carried through.
    pre = "azure credentials OK\nload variant mapping: no such file"
    assert "load variant mapping" in summary.summarize("/health", pre)
    print("PASS test_health_all_passed_and_prefail")


def _validate_report() -> dict:
    """Shaped like a real `dreadgoad validate` run: 46 categories, mixed states."""
    checks = [
        {"status": "PASS", "category": "Discovery", "name": "Found DC01"},
        {"status": "PASS", "category": "Discovery", "name": "Found DC02"},
        {
            "status": "FAIL",
            "category": "Audit",
            "name": "LDAP Field Engineering=0 on DC01",
        },
        {
            "status": "FAIL",
            "category": "Audit",
            "name": "LDAP Field Engineering=0 on DC02",
        },
        {"status": "PASS", "category": "Audit", "name": "Audit policy set"},
        {"status": "SKIP", "category": "Defender", "name": "not configured"},
        {"status": "INFO", "category": "SMBv1", "name": "not configured"},
    ]
    return {
        "environment": "dreadindex",
        "total_checks": 7,
        "passed": 3,
        "failed": 2,
        "warnings": 0,
        "checks": checks,
    }


def test_validate_report_read_from_saved_path() -> None:
    """The report is loaded from the path validate prints, not from stdout."""
    import tempfile

    report = _validate_report()
    with tempfile.TemporaryDirectory() as d:
        path = pathlib.Path(d) / "goad-validation-20260805-130436.json"
        path.write_text(json.dumps(report))
        out = f"some table noise\nResults saved to: {path}\nvalidation failed with 2 errors"
        got = summary.parse_validate_report(out)
        assert got is not None and got["total_checks"] == 7, got

        # No path line at all → None (falls back to raw output).
        assert summary.parse_validate_report("no path here") is None
        # Path present but the file is gone → None, not an exception.
        path.unlink()
        assert summary.parse_validate_report(out) is None
    print("PASS test_validate_report_read_from_saved_path")


def test_validate_report_rejects_arbitrary_paths() -> None:
    """Only goad-validation-*.json is ever opened, whatever the output claims."""
    import tempfile

    with tempfile.TemporaryDirectory() as d:
        evil = pathlib.Path(d) / "id_rsa"
        evil.write_text('{"checks": []}')
        assert summary.parse_validate_report(f"Results saved to: {evil}") is None
        assert summary.parse_validate_report("Results saved to: /etc/passwd") is None
        # Traversal is rejected only when it lands on a non-matching basename —
        # the guard constrains the *filename*, not the directory. That's the
        # documented limit: a path claiming goad-validation-*.json in any
        # directory is still read. Acceptable because the path comes from our
        # own CLI's stdout, and the filename constraint stops it pointing at
        # arbitrary secrets (keys, /etc/passwd) even if that output were spoofed.
        assert (
            summary.parse_validate_report("Results saved to: /tmp/../etc/goad-x.json")
            is None
        )
    print("PASS test_validate_report_rejects_arbitrary_paths")


def test_describe_exit_separates_cancel_from_failure() -> None:
    """Regression: a cancelled /validate was reported to the agent as exit -2.

    The model then tried to explain a "failure" whose output was simply cut
    short mid-run, and told the operator the range had errors when it hadn't.
    """
    assert summary.describe_exit(0) == "succeeded"
    # Real failures keep their code.
    assert summary.describe_exit(1) == "failed (exit 1)"
    assert summary.describe_exit(3) == "failed (exit 3)"
    # -2 is SIGINT: the operator pressed Esc. Must not read as a failure.
    cancelled = summary.describe_exit(-2)
    assert "cancelled" in cancelled, cancelled
    assert "failed" not in cancelled, cancelled
    assert "incomplete" in cancelled, "the model must know the output is partial"
    # Other signals are terminations, also not failures.
    for sig, code in (("SIGTERM", -15), ("SIGKILL", -9)):
        got = summary.describe_exit(code)
        assert sig in got and "failed" not in got, got
        assert "incomplete" in got, got
    # An unknown signal number still degrades gracefully.
    assert "signal 99" in summary.describe_exit(-99), summary.describe_exit(-99)
    print("PASS test_describe_exit_separates_cancel_from_failure")


def test_strip_ansi_covers_agent_and_chat() -> None:
    """The model must not read escape codes as content (validate draws a table)."""
    esc = "\x1b"
    coloured = f"{esc}[38;2;202;94;68m│{esc}[m  {esc}[1;38;5;9m[x] {esc}[mAudit 3/6"
    assert summary.strip_ansi(coloured) == "│  [x] Audit 3/6", summary.strip_ansi(
        coloured
    )
    # Reaches the agent's view too, on the unstructured fallback path.
    assert esc not in summary.summarize("/up", coloured), "agent still sees ANSI"
    assert "Audit 3/6" in summary.summarize("/up", coloured)
    # Plain text is untouched.
    assert summary.strip_ansi("no escapes here") == "no escapes here"
    print("PASS test_strip_ansi_covers_agent_and_chat")


def test_validate_categories_rollup_and_ordering() -> None:
    rows = summary.validate_categories(_validate_report()["checks"])
    by = {r["category"]: r for r in rows}
    assert by["Audit"]["state"] == "failed", by["Audit"]
    assert by["Audit"] == {
        "category": "Audit",
        "state": "failed",
        "passed": 1,
        "failed": 2,
        "total": 3,
    }
    assert by["Discovery"]["state"] == "passed", by["Discovery"]
    # INFO/SKIP-only categories asserted nothing — not failures.
    assert by["Defender"]["state"] == "skipped", by["Defender"]
    assert by["SMBv1"]["state"] == "skipped", by["SMBv1"]
    # Failures sort first so the eye lands on them.
    assert rows[0]["category"] == "Audit", [r["category"] for r in rows]
    print("PASS test_validate_categories_rollup_and_ordering")


def test_summarize_validate_leads_with_failures() -> None:
    out = summary.summarize_validate(_validate_report())
    assert out.startswith("validate: 3 passed, 2 failed, 0 warnings (7 checks)"), out
    assert "Audit: LDAP Field Engineering=0 on DC01" in out, out
    assert "Found DC01" not in out, "passing checks must not be enumerated"
    assert "not fully configured:" in out and "Defender" in out, out
    # A clean run says so rather than listing 200 passes.
    clean = {
        "passed": 5,
        "failed": 0,
        "warnings": 0,
        "total_checks": 5,
        "checks": [{"status": "PASS", "category": "X", "name": "ok"}],
    }
    assert "no failed checks." in summary.summarize_validate(clean)
    print("PASS test_summarize_validate_leads_with_failures")


def test_scrub_report_parsing() -> None:
    """score reset has no --json verb, so the per-host block is parsed."""
    out = (
        "=== DreadGOAD score reset (env=dreadindex) ===\n"
        "    mode=dry-run  skip_kali=false\n\n"
        "--- Phase 2: Windows hosts ---\n"
        "=== SOLAR ===\n"
        "  clean (no artifacts)\n"
        "=== QUANTUM-WEB ===\n"
        "  Total: 3 issues found, 3 would remove\n"
        "=== SUMMIT ===\n"
        "  Total: 1 issues found, 0 would remove\n"
        "  ERROR: access denied\n"
        "=== score reset complete ==="
    )
    r = summary.parse_scrub_report(out)
    assert r is not None
    assert r["mode"] == "dry-run", r
    names = [h["host"] for h in r["hosts"]]
    # The banner and the completion line share the "=== x ===" shape and must
    # not become phantom hosts.
    assert names == ["SOLAR", "QUANTUM-WEB", "SUMMIT"], names
    by = {h["host"]: h for h in r["hosts"]}
    assert by["SOLAR"]["clean"] is True and by["SOLAR"]["found"] == 0
    assert by["QUANTUM-WEB"]["found"] == 3 and by["QUANTUM-WEB"]["removed"] == 3
    assert by["SUMMIT"]["errors"] == ["access denied"], by["SUMMIT"]

    # Colourised output must parse too — the summary lines are ANSI-wrapped.
    esc = "\x1b[32m"
    coloured = out.replace(
        "  clean (no artifacts)", f"{esc}  clean (no artifacts)\x1b[0m"
    )
    parsed = summary.parse_scrub_report(coloured)
    assert parsed is not None, "ANSI-wrapped output must still parse"
    assert parsed["hosts"][0]["clean"] is True

    # Not a scrub / failed early -> None, so summarize falls back to raw output.
    assert summary.parse_scrub_report("some unrelated output") is None
    print("PASS test_scrub_report_parsing")


def test_summarize_scrub_leads_with_dirty_hosts() -> None:
    out = summary.summarize_scrub(
        {
            "mode": "dry-run",
            "hosts": [
                {"host": "A", "found": 0, "removed": 0, "clean": True, "errors": []},
                {"host": "B", "found": 2, "removed": 2, "clean": False, "errors": []},
            ],
        }
    )
    assert out.startswith("scrub (dry-run): 2 hosts checked, 2 artifacts found"), out
    assert "B: 2 found, 2 would remove" in out, out
    assert "\n  A:" not in out, "clean hosts must not be enumerated"
    assert "(1 clean host not listed)" in out, out
    # apply mode changes the verb, since "would remove" would be a lie.
    applied = summary.summarize_scrub(
        {
            "mode": "apply",
            "hosts": [
                {"host": "B", "found": 1, "removed": 1, "clean": False, "errors": []}
            ],
        }
    )
    assert "1 removed" in applied and "would remove" not in applied, applied
    # All clean says so rather than listing nothing.
    assert "all hosts clean." in summary.summarize_scrub(
        {
            "mode": "dry-run",
            "hosts": [
                {"host": "A", "found": 0, "removed": 0, "clean": True, "errors": []}
            ],
        }
    )
    print("PASS test_summarize_scrub_leads_with_dirty_hosts")


def test_clip_keeps_head_and_tail_and_marks_the_gap() -> None:
    text = "\n".join(f"line{i:04d}" for i in range(2000))
    out = summary.clip(text, limit=400)
    assert "line0000" in out, "head lost"
    assert "line1999" in out, "tail lost — the error usually lives here"
    assert "omitted from the middle" in out, "silent truncation is the bug"
    assert len(out) <= 500, len(out)
    # The marker must state a truthful count.
    marker = [ln for ln in out.splitlines() if "omitted" in ln][0]
    omitted = int(marker.split("[")[1].split(" ")[0])
    assert omitted == 2000 - (len(out.splitlines()) - 1), marker
    print("PASS test_clip_keeps_head_and_tail_and_marks_the_gap")


def test_clip_edge_cases() -> None:
    assert summary.clip("short", limit=100) == "short", "under budget → untouched"
    assert summary.clip("", limit=100) == ""
    # One enormous line with no newline to cut on.
    giant = "x" * 5000
    out = summary.clip(giant, limit=400)
    assert "characters omitted" in out, out[:120]
    assert len(out) <= 500, len(out)
    # Exactly at the limit → untouched.
    exact = "y" * 100
    assert summary.clip(exact, limit=100) == exact
    print("PASS test_clip_edge_cases")


def test_command_parser_shares_one_implementation() -> None:
    """The command overlay and agent path must parse output identically."""
    raw = json.dumps(_instances(3), indent=2)
    assert command_runner.parse_instances(raw) == summary.parse_json_array(raw)
    assert command_runner.parse_instances("no json") is None
    assert command_runner.parse_instances("[]") == []
    # A JSON object (not an array) is not an instance list.
    assert command_runner.parse_instances('{"a": 1}') is None
    print("PASS test_command_parser_shares_one_implementation")


def test_unknown_command_falls_back_to_clip() -> None:
    text = "\n".join(f"terraform line {i}" for i in range(5000))
    out = summary.summarize("/up", text)
    assert len(out) <= summary.DEFAULT_LIMIT + 200, len(out)
    assert "omitted from the middle" in out
    print("PASS test_unknown_command_falls_back_to_clip")


if __name__ == "__main__":
    test_regression_seven_vms_all_survive()
    test_instances_scale_and_states()
    test_instances_surface_cloud_placement()
    test_summarize_exec_per_host_blocks()
    test_summarize_exec_flags_truncated_output()
    test_exec_decodes_utf16_from_a_stale_cli()
    test_exec_success_vocabulary_matches_the_cli()
    test_summarize_exec_strips_ansi_from_host_output()
    test_summarize_exec_edge_cases()
    test_instances_empty_and_unparseable()
    test_non_object_array_falls_back_instead_of_raising()
    test_validate_report_read_from_saved_path()
    test_validate_report_rejects_arbitrary_paths()
    test_describe_exit_separates_cancel_from_failure()
    test_strip_ansi_covers_agent_and_chat()
    test_validate_categories_rollup_and_ordering()
    test_summarize_validate_leads_with_failures()
    test_health_lists_failures_not_passes()
    test_health_all_passed_and_prefail()
    test_scrub_report_parsing()
    test_summarize_scrub_leads_with_dirty_hosts()
    test_clip_keeps_head_and_tail_and_marks_the_gap()
    test_clip_edge_cases()
    test_command_parser_shares_one_implementation()
    test_unknown_command_falls_back_to_clip()
    print("ALL PASS")
