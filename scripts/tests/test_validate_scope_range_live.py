"""Unit tests for the standalone SCOPE-RANGE live validator."""

from __future__ import annotations

import contextlib
import importlib.util
import io
import json
import pathlib
import sys
import tempfile
import unittest


REPO_ROOT = pathlib.Path(__file__).resolve().parents[2]
VALIDATOR_PATH = REPO_ROOT / "scripts" / "validate-scope-range-live.py"
SPEC = importlib.util.spec_from_file_location("scope_range_validator", VALIDATOR_PATH)
if SPEC is None or SPEC.loader is None:  # pragma: no cover - import setup guard
    raise RuntimeError(f"could not load {VALIDATOR_PATH}")
validator = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = validator
SPEC.loader.exec_module(validator)


class FakeAzure:
    """Minimal AzureCLI stand-in for remote-result tests."""

    def __init__(self, response: object | list[object]) -> None:
        self.responses = response if isinstance(response, list) else [response]
        self.calls: list[tuple[list[str], int]] = []

    def run_json(self, args: list[str], *, timeout: int) -> object:
        """Record one invocation and return its configured response."""
        self.calls.append((args, timeout))
        return self.responses[len(self.calls) - 1]


class InfrastructureAzure:
    """Azure stand-in with a complete six-host network topology."""

    def __init__(
        self,
        manifest: dict[str, object],
        *,
        wrong_nat: bool = False,
        missing_nat_public_ip: bool = False,
    ) -> None:
        self.manifest = manifest
        self.wrong_nat = wrong_nat
        self.missing_nat_public_ip = missing_nat_public_ip

    def run_json(self, args: list[str], *, timeout: int = 600) -> object:
        """Return the Azure object selected by the requested command."""
        del timeout
        env = self.manifest["default_environment"]
        network = self.manifest["network"]
        if args[:2] == ["account", "show"]:
            return {"name": "test", "id": "subscription"}
        if args[:2] == ["group", "show"]:
            return {"properties": {"provisioningState": "Succeeded"}}
        if args[:2] == ["vm", "list"]:
            return [
                {
                    "name": validator.render_template(host["vm_name_template"], env),
                    "powerState": "VM running",
                    "privateIps": host["private_ip"],
                    "publicIps": "",
                    "hardwareProfile": {"vmSize": host["size"]},
                    "tags": host["tags"],
                }
                for host in self.manifest["hosts"]
            ]
        if args[:3] == ["network", "vnet", "show"]:
            gateway = validator.render_template(
                network["nat_gateway_name_template"], env
            )
            if self.wrong_nat:
                gateway = "unexpected-nat"
            return {
                "addressSpace": {"addressPrefixes": [network["vnet_cidr"]]},
                "subnets": [
                    {
                        "name": validator.render_template(
                            network["workload_subnet_name_template"], env
                        ),
                        "addressPrefixes": [network["workload_subnet"]],
                        "natGateway": {"id": f"/natGateways/{gateway}"},
                    },
                    {
                        "name": "AzureBastionSubnet",
                        "addressPrefixes": [network["bastion_subnet"]],
                    },
                ],
            }
        if args[:4] == ["network", "nat", "gateway", "show"]:
            public_ip = validator.render_template(
                network["nat_public_ip_name_template"], env
            )
            return {
                "provisioningState": "Succeeded",
                "publicIpAddresses": (
                    None
                    if self.missing_nat_public_ip
                    else [{"id": f"/publicIPAddresses/{public_ip}"}]
                ),
            }
        if args[:3] == ["network", "public-ip", "list"]:
            return [
                {"name": validator.render_template(template, env)}
                for template in network["expected_public_ip_names"]
            ]
        if args[:3] == ["network", "bastion", "show"]:
            return {"sku": {"name": "Standard"}, "enableTunneling": True}
        raise AssertionError(f"unexpected Azure command: {args}")


class ManifestTests(unittest.TestCase):
    """Exercise the deployed-state contract and its validation."""

    def setUp(self) -> None:
        self.manifest = validator.load_manifest(validator.DEFAULT_MANIFEST)

    def test_real_manifest_covers_all_six_hosts(self) -> None:
        hosts = {host["id"]: host for host in self.manifest["hosts"]}

        self.assertEqual(
            set(hosts),
            {"kali01", "web01", "data01", "dev01", "storage01", "services01"},
        )
        self.assertEqual(sum(len(host["checks"]) for host in hosts.values()), 84)
        self.assertTrue(
            all(
                validator.select_host_checks(host, True)["checks"]
                for host in hosts.values()
            )
        )
        check_names = {
            check["name"] for host in hosts.values() for check in host["checks"]
        }
        self.assertTrue(
            {
                "private subnet has functional outbound HTTPS",
                "external storage passes Nextcloud verification",
                "remaining PostgreSQL and Redis records are seeded",
                "Actions runner is registered and recently online",
                "synthetic account can read shared data over SFTP",
                "synthetic users can bind and have expected group membership",
                "queue worker is consuming from range-jobs",
                "synthetic mail users authenticate through Dovecot",
                "headless browser reaches seeded web applications",
                "seeded Garage object is readable through WebDAV",
                "all versioned queue jobs have completion markers",
                "ORCHID issue tracking fixtures are exact",
                "seeded credentials and successful export build are retained",
                "three database backups are usable",
                "three cross-host exports contain versioned data",
                "six versioned collaboration messages are present",
            }.issubset(check_names)
        )
        persistent_mounts = [
            check
            for host in hosts.values()
            for check in host["checks"]
            if check["name"] == "range data disk is mounted persistently"
        ]
        self.assertEqual(len(persistent_mounts), 5)
        self.assertTrue(
            all("/etc/fstab" in check["command"] for check in persistent_mounts)
        )

    def test_duplicate_host_is_rejected(self) -> None:
        manifest = json.loads(json.dumps(self.manifest))
        manifest["hosts"].append(manifest["hosts"][0])
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "manifest.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "duplicate host id"):
                validator.load_manifest(path)

    def test_invalid_quick_flag_is_rejected(self) -> None:
        manifest = json.loads(json.dumps(self.manifest))
        manifest["hosts"][0]["checks"][0]["quick"] = "yes"
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "manifest.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "invalid quick flag"):
                validator.load_manifest(path)

    def test_extra_template_field_is_rejected(self) -> None:
        manifest = json.loads(json.dumps(self.manifest))
        manifest["resource_group_template"] = "{env}-{0}-rg"
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "manifest.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "resource_group_template"):
                validator.load_manifest(path)

    def test_manifest_only_does_not_require_azure_cli(self) -> None:
        stdout = io.StringIO()
        with contextlib.redirect_stdout(stdout):
            status = validator.main(["--manifest-only"])

        self.assertEqual(status, 0)
        self.assertIn("manifest is valid", stdout.getvalue())


class RemoteExecutionTests(unittest.TestCase):
    """Verify safe payload construction and result handling."""

    def setUp(self) -> None:
        self.host = {
            "id": "data01",
            "checks": [
                {
                    "category": "Data",
                    "name": "first check",
                    "quick": True,
                    "command": "test secret-value = secret-value",
                },
                {
                    "category": "Data",
                    "name": "second check",
                    "quick": False,
                    "command": "true",
                },
            ],
        }

    def test_launcher_does_not_expose_plaintext_check_content(self) -> None:
        launcher = validator.build_remote_launcher(self.host, quick=False)

        self.assertNotIn("secret-value", launcher)
        self.assertNotIn("first check", launcher)
        self.assertIn("base64 -d | bash", launcher)

    def test_remote_commands_cannot_consume_the_check_stream(self) -> None:
        self.assertIn(
            'bash -o pipefail -c "$command_text" </dev/null', validator.REMOTE_RUNNER
        )

    def test_verbose_command_redacts_remote_payload(self) -> None:
        command = ["az", "vm", "run-command", "invoke", "--scripts", "encoded-secret"]

        redacted = validator.redact_azure_command(command)

        self.assertEqual(redacted[-1], "<remote-validation-payload>")
        self.assertNotIn("encoded-secret", redacted)
        self.assertEqual(
            command[-1], "encoded-secret", "input command must not be mutated"
        )

    def test_remote_parser_ignores_wrapper_and_invalid_lines(self) -> None:
        message = "\n".join(
            [
                "Enable succeeded: [stdout]",
                'SCOPE_RESULT {"status":"PASS","category":"Data","name":"database","detail":"ok"}',
                "SCOPE_RESULT not-json",
                'SCOPE_RESULT {"status":"UNKNOWN","category":"Data","name":"ignored"}',
                "[stderr]",
            ]
        )

        parsed = validator.parse_remote_results(message, "data01")

        self.assertEqual(len(parsed), 1)
        self.assertEqual(parsed[0].status, "PASS")
        self.assertEqual(parsed[0].host, "data01")

    def test_missing_remote_results_are_reported(self) -> None:
        response = {
            "value": [
                {
                    "message": (
                        '[stdout]\nSCOPE_RESULT {"status":"PASS","category":"Data",'
                        '"name":"first check","detail":"expected state present"}'
                    )
                }
            ]
        }
        azure = FakeAzure(response)

        checks = validator.run_host_checks(
            azure,
            self.host,
            "scope-dev-data01",
            "scope-dev-scope-range-rg",
            quick=False,
        )

        self.assertEqual(len(azure.calls), 1)
        self.assertEqual([check.status for check in checks], ["PASS", "FAIL"])
        self.assertIn("expected=2 actual=1", checks[-1].detail)

    def test_duplicate_result_cannot_hide_a_missing_check(self) -> None:
        duplicate = (
            'SCOPE_RESULT {"status":"PASS","category":"Data",'
            '"name":"first check","detail":"expected state present"}'
        )
        azure = FakeAzure({"value": [{"message": f"{duplicate}\n{duplicate}"}]})

        checks = validator.run_host_checks(
            azure,
            self.host,
            "scope-dev-data01",
            "scope-dev-scope-range-rg",
            quick=False,
        )

        self.assertEqual(len(checks), 3)
        self.assertEqual(checks[-1].status, "FAIL")
        self.assertIn("missing=Data/second check", checks[-1].detail)
        self.assertIn("unexpected=Data/first check", checks[-1].detail)

    def test_remote_checks_are_split_into_bounded_batches(self) -> None:
        host = {
            "id": "data01",
            "checks": [
                {
                    "category": "Data",
                    "name": f"check {index}",
                    "quick": True,
                    "command": "true",
                }
                for index in range(6)
            ],
        }

        def response_for(checks: list[dict[str, object]]) -> dict[str, object]:
            lines = [
                "SCOPE_RESULT "
                + json.dumps(
                    {
                        "status": "PASS",
                        "category": check["category"],
                        "name": check["name"],
                        "detail": "expected state present",
                    }
                )
                for check in checks
            ]
            return {"value": [{"message": "\n".join(lines)}]}

        azure = FakeAzure(
            [
                response_for(host["checks"][: validator.REMOTE_BATCH_SIZE]),
                response_for(host["checks"][validator.REMOTE_BATCH_SIZE :]),
            ]
        )

        checks = validator.run_host_checks(
            azure,
            host,
            "scope-dev-data01",
            "scope-dev-scope-range-rg",
            quick=False,
        )

        self.assertEqual(len(azure.calls), 2)
        self.assertEqual(len(checks), 6)
        self.assertTrue(all(check.status == "PASS" for check in checks))


class InfrastructureTests(unittest.TestCase):
    """Verify the Azure topology checks introduced for the NAT path."""

    def setUp(self) -> None:
        self.manifest = validator.load_manifest(validator.DEFAULT_MANIFEST)

    def test_expected_nat_gateway_attachment_passes(self) -> None:
        checks, runnable = validator.validate_infrastructure(
            InfrastructureAzure(self.manifest),
            self.manifest,
            self.manifest["default_environment"],
            "scope-dev-scope-range-rg",
        )

        nat_check = next(
            check
            for check in checks
            if check.name == "workload subnet uses the expected NAT gateway"
        )
        self.assertEqual(nat_check.status, "PASS")
        self.assertEqual(len(runnable), 6)

    def test_wrong_nat_gateway_attachment_fails(self) -> None:
        checks, _ = validator.validate_infrastructure(
            InfrastructureAzure(self.manifest, wrong_nat=True),
            self.manifest,
            self.manifest["default_environment"],
            "scope-dev-scope-range-rg",
        )

        nat_check = next(
            check
            for check in checks
            if check.name == "workload subnet uses the expected NAT gateway"
        )
        self.assertEqual(nat_check.status, "FAIL")

    def test_missing_nat_public_ip_fails_without_crashing(self) -> None:
        checks, _ = validator.validate_infrastructure(
            InfrastructureAzure(self.manifest, missing_nat_public_ip=True),
            self.manifest,
            self.manifest["default_environment"],
            "scope-dev-scope-range-rg",
        )

        nat_check = next(
            check
            for check in checks
            if check.name == "workload subnet uses the expected NAT gateway"
        )
        self.assertEqual(nat_check.status, "FAIL")
        self.assertIn("public_ips=none", nat_check.detail)


class ReportTests(unittest.TestCase):
    """Verify deterministic summaries and atomic report output."""

    def test_report_counts_and_round_trips(self) -> None:
        checks = [
            validator.result("PASS", "Data", "available"),
            validator.result("FAIL", "Data", "seeded"),
            validator.result("WARN", "Network", "optional"),
        ]
        report = validator.build_report(
            checks,
            "scope-dev",
            "scope-dev-scope-range-rg",
            "subscription-id",
            quick=True,
        )

        self.assertEqual(
            (
                report["total_checks"],
                report["passed"],
                report["failed"],
                report["warnings"],
            ),
            (3, 1, 1, 1),
        )
        self.assertEqual(report["mode"], "quick")
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "report.json"
            validator.write_report(path, report)
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), report)
            self.assertEqual(path.stat().st_mode & 0o777, 0o644)


if __name__ == "__main__":
    unittest.main()
