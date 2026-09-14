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
from typing import Any


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

    def run_json(self, args: list[str], *, timeout: int = 600) -> object:
        """Record one invocation and return its configured response."""
        self.calls.append((args, timeout))
        return self.responses[len(self.calls) - 1]


class InfrastructureAzure:
    """Azure stand-in with a complete six-host network topology."""

    def __init__(
        self,
        manifest: dict[str, Any],
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


class InfrastructureAWS:
    """AWS stand-in with a complete six-host private topology."""

    def __init__(
        self,
        manifest: dict[str, Any],
        *,
        public_host: bool = False,
        duplicate_host: bool = False,
    ) -> None:
        self.manifest = manifest
        self.public_host = public_host
        self.duplicate_host = duplicate_host
        self.calls: list[list[str]] = []

    def run_json(self, args: list[str], *, timeout: int = 600) -> object:
        """Return the AWS object selected by the requested command."""
        del timeout
        self.calls.append(args)
        env = self.manifest["aws"]["default_environment"]
        if args[:2] == ["sts", "get-caller-identity"]:
            return {
                "Account": "123456789012",
                "Arn": "arn:aws:iam::123456789012:user/test",
            }
        if args[:2] == ["ec2", "describe-instances"]:
            instances = []
            for host in self.manifest["hosts"]:
                tags = {
                    **host["tags"],
                    "Name": validator.render_aws_instance_name(
                        self.manifest, host["id"], env
                    ),
                    "Project": "DreadGOAD",
                    "Environment": env,
                    "OS": "Linux",
                }
                instance = {
                    "InstanceId": f"i-{host['id']}",
                    "InstanceType": host["aws_instance_type"],
                    "PrivateIpAddress": host["private_ip"],
                    "State": {"Name": "running"},
                    "Tags": [
                        {"Key": key, "Value": value} for key, value in tags.items()
                    ],
                }
                if self.public_host and host["id"] == "web01":
                    instance["PublicIpAddress"] = "203.0.113.10"
                instances.append(instance)
                if self.duplicate_host and host["id"] == "web01":
                    duplicate = dict(instance)
                    duplicate["InstanceId"] = "i-web01-orphan"
                    instances.append(duplicate)
            return {"Reservations": [{"Instances": instances}]}
        if args[:2] == ["ssm", "describe-instance-information"]:
            return {
                "InstanceInformationList": [
                    {"InstanceId": f"i-{host['id']}", "PingStatus": "Online"}
                    for host in self.manifest["hosts"]
                ]
            }
        if args[:2] == ["ec2", "describe-vpcs"]:
            return {"Vpcs": [{"VpcId": "vpc-goat", "CidrBlock": "10.50.0.0/16"}]}
        if args[:2] == ["ec2", "describe-subnets"]:
            return {
                "Subnets": [
                    {
                        "SubnetId": "subnet-public",
                        "CidrBlock": "10.50.0.0/24",
                        "MapPublicIpOnLaunch": True,
                        "Tags": [{"Key": "Type", "Value": "public"}],
                    },
                    {
                        "SubnetId": "subnet-private",
                        "CidrBlock": "10.50.10.0/24",
                        "MapPublicIpOnLaunch": False,
                        "Tags": [{"Key": "Type", "Value": "private"}],
                    },
                ]
            }
        if args[:2] == ["ec2", "describe-nat-gateways"]:
            return {"NatGateways": [{"NatGatewayId": "nat-goat"}]}
        if args[:2] == ["ec2", "describe-route-tables"]:
            return {
                "RouteTables": [
                    {
                        "Tags": [{"Key": "Name", "Value": f"{env}-goat"}],
                        "Routes": [
                            {
                                "DestinationCidrBlock": "0.0.0.0/0",
                                "NatGatewayId": "nat-goat",
                            }
                        ],
                        "Associations": [{"SubnetId": "subnet-private"}],
                    }
                ]
            }
        if args[:2] == ["ec2", "describe-vpc-endpoints"]:
            return {
                "VpcEndpoints": [
                    {
                        "ServiceName": f"com.amazonaws.us-east-2.{service}",
                        "State": "available",
                    }
                    for service in self.manifest["aws"]["required_vpc_endpoints"]
                ]
            }
        if args[:2] == ["ec2", "describe-security-groups"]:
            return {
                "SecurityGroups": [
                    {
                        "GroupId": "sg-goat",
                        "IpPermissions": [{"IpRanges": [{"CidrIp": "10.50.0.0/16"}]}],
                    }
                ]
            }
        raise AssertionError(f"unexpected AWS command: {args}")


class ManifestTests(unittest.TestCase):
    """Exercise the deployed-state contract and its validation."""

    def setUp(self) -> None:
        self.manifest = validator.load_manifest(validator.DEFAULT_MANIFEST)

    def test_real_manifest_covers_all_six_hosts(self) -> None:
        hosts = {host["id"]: host for host in self.manifest["hosts"]}

        self.assertEqual(self.manifest["deployment_name"], "goat")
        self.assertEqual(set(self.manifest["providers"]), {"azure", "aws"})
        self.assertEqual(self.manifest["aws"]["default_region"], "us-east-2")
        deployment = validator.resolve_deployment_name(
            self.manifest, self.manifest["default_environment"]
        )
        self.assertEqual(deployment, "goat")
        self.assertEqual(
            validator.render_template(
                self.manifest["resource_group_template"],
                self.manifest["default_environment"],
                deployment,
            ),
            "scope-dev-goat-rg",
        )
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
        health_counts = {
            host_id: len(
                validator.select_host_checks(host, quick=False, health=True)["checks"]
            )
            for host_id, host in hosts.items()
        }
        self.assertTrue(
            all(count > 0 for count in health_counts.values()), health_counts
        )
        self.assertLess(sum(health_counts.values()), 20, health_counts)
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
                "KRAKEN issue tracking fixtures are exact",
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

    def test_environment_hcl_preserves_legacy_resource_prefix(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            infra_root = pathlib.Path(temp_dir)
            env_dir = infra_root / "legacy"
            env_dir.mkdir()
            (env_dir / "env.hcl").write_text(
                'locals {\n  deployment_name = "scope-range"\n}\n',
                encoding="utf-8",
            )

            self.assertEqual(
                validator.resolve_deployment_name(self.manifest, "legacy", infra_root),
                "scope-range",
            )

    def test_missing_environment_hcl_uses_goat_prefix(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            self.assertEqual(
                validator.resolve_deployment_name(
                    self.manifest, "new-range", pathlib.Path(temp_dir)
                ),
                "goat",
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

    def test_invalid_health_flag_is_rejected(self) -> None:
        manifest = json.loads(json.dumps(self.manifest))
        manifest["hosts"][0]["checks"][0]["health"] = "yes"
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "manifest.json"
            path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "invalid health flag"):
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
                    "health": True,
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

    def test_health_subset_is_explicit_and_exclusive(self) -> None:
        selected = validator.select_host_checks(self.host, quick=False, health=True)[
            "checks"
        ]
        self.assertEqual([check["name"] for check in selected], ["first check"])
        with self.assertRaisesRegex(ValueError, "mutually exclusive"):
            validator.select_host_checks(self.host, quick=True, health=True)

    def test_remote_commands_cannot_consume_the_check_stream(self) -> None:
        self.assertIn(
            'bash -o pipefail -c "$command_text" </dev/null', validator.REMOTE_RUNNER
        )

    def test_aws_remote_checks_use_linux_ssm_document(self) -> None:
        line = (
            'SCOPE_RESULT {"status":"PASS","category":"Data",'
            '"name":"first check","detail":"expected state present"}'
        )
        aws = FakeAzure(
            [
                {"Command": {"CommandId": "command-1"}},
                {"Status": "Success", "StandardOutputContent": line},
            ]
        )

        checks = validator.run_aws_host_checks(aws, self.host, "i-data01", quick=True)

        self.assertEqual([check.status for check in checks], ["PASS"])
        send_args = aws.calls[0][0]
        self.assertIn("AWS-RunShellScript", send_args)
        self.assertNotIn("AWS-RunPowerShellScript", send_args)

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
            "scope-dev-goat-rg",
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
            "scope-dev-goat-rg",
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
            "scope-dev-goat-rg",
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
            "scope-dev-goat-rg",
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
            "scope-dev-goat-rg",
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
            "scope-dev-goat-rg",
        )

        nat_check = next(
            check
            for check in checks
            if check.name == "workload subnet uses the expected NAT gateway"
        )
        self.assertEqual(nat_check.status, "FAIL")
        self.assertIn("public_ips=none", nat_check.detail)

    def test_expected_aws_private_topology_passes(self) -> None:
        aws = InfrastructureAWS(self.manifest)
        checks, runnable = validator.validate_aws_infrastructure(
            aws,
            self.manifest,
            self.manifest["aws"]["default_environment"],
        )
        self.assertEqual(len(runnable), 6)
        self.assertFalse(
            [check for check in checks if check.status == "FAIL"],
            [(check.name, check.detail) for check in checks if check.status == "FAIL"],
        )
        vpc_call = next(
            call for call in aws.calls if call[:2] == ["ec2", "describe-vpcs"]
        )
        self.assertIn("Name=tag:Range,Values=GOAT", vpc_call)
        self.assertFalse(any("tag:Lab" in argument for argument in vpc_call))

    def test_aws_public_workload_address_fails(self) -> None:
        checks, _ = validator.validate_aws_infrastructure(
            InfrastructureAWS(self.manifest, public_host=True),
            self.manifest,
            self.manifest["aws"]["default_environment"],
        )
        public_check = next(
            check
            for check in checks
            if check.host == "web01"
            and check.name == "workload instance has no public IP"
        )
        self.assertEqual(public_check.status, "FAIL")

    def test_aws_duplicate_name_tag_fails_exact_instance_set(self) -> None:
        checks, _ = validator.validate_aws_infrastructure(
            InfrastructureAWS(self.manifest, duplicate_host=True),
            self.manifest,
            self.manifest["aws"]["default_environment"],
        )
        exact_check = next(
            check for check in checks if check.name == "exact EC2 instance set"
        )
        self.assertEqual(exact_check.status, "FAIL")
        self.assertIn("scope-aws-goat-web01': 2", exact_check.detail)


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
            "scope-dev-goat-rg",
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
        self.assertEqual(report["report_type"], "validation")
        with tempfile.TemporaryDirectory() as temp_dir:
            path = pathlib.Path(temp_dir) / "report.json"
            validator.write_report(path, report)
            self.assertEqual(json.loads(path.read_text(encoding="utf-8")), report)
            self.assertEqual(path.stat().st_mode & 0o777, 0o644)


if __name__ == "__main__":
    unittest.main()
