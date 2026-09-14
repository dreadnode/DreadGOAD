#!/usr/bin/env python3
"""Validate a deployed Azure or AWS SCOPE-RANGE against its expected-state manifest."""

from __future__ import annotations

import argparse
import base64
import collections
import concurrent.futures
import dataclasses
import datetime as dt
import json
import os
import pathlib
import re
import shlex
import shutil
import string
import subprocess
import sys
import tempfile
import time
from typing import Any, Sequence


SCRIPT_DIR = pathlib.Path(__file__).resolve().parent
DEFAULT_MANIFEST = SCRIPT_DIR.parent / "ad" / "SCOPE-RANGE" / "data" / "validation.json"
DEFAULT_AZURE_INFRA_ROOT = (
    SCRIPT_DIR.parent / "infra" / "azure" / "scope-range-deployment"
)
DEFAULT_AWS_INFRA_ROOT = SCRIPT_DIR.parent / "infra" / "scope-range-deployment"
ENV_NAME_PATTERN = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_-]*$")
DEPLOYMENT_NAME_PATTERN = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_-]*$")
DEPLOYMENT_HCL_PATTERN = re.compile(
    r'^\s*deployment_name\s*=\s*"([a-zA-Z0-9][a-zA-Z0-9_-]*)"\s*(?:#.*)?$',
    re.MULTILINE,
)
REMOTE_RESULT_PREFIX = "SCOPE_RESULT "
AZURE_TIMEOUT_SECONDS = 600
AWS_TIMEOUT_SECONDS = 600
REMOTE_BATCH_SIZE = 5
REQUIRED_NETWORK_FIELDS = {
    "vnet_name_template",
    "workload_subnet_name_template",
    "nat_gateway_name_template",
    "nat_public_ip_name_template",
    "vnet_cidr",
    "workload_subnet",
    "bastion_subnet",
    "expected_public_ip_names",
}


REMOTE_RUNNER = r"""#!/usr/bin/env bash
set -uo pipefail

spec="$(printf '%s' "$SCOPE_VALIDATION_SPEC_B64" | base64 -d)" || exit 2
host="$(printf '%s' "$spec" | jq -er '.id')" || exit 2

emit_result() {
  local status="$1"
  local category="$2"
  local name="$3"
  local detail="$4"
  printf 'SCOPE_RESULT '
  jq -cn \
    --arg status "$status" \
    --arg category "$category" \
    --arg name "$name" \
    --arg detail "$detail" \
    --arg host "$host" \
    '{status: $status, category: $category, name: $name, detail: $detail, host: $host}'
}

while IFS= read -r check; do
  category="$(printf '%s' "$check" | jq -er '.category')" || continue
  name="$(printf '%s' "$check" | jq -er '.name')" || continue
  command_text="$(printf '%s' "$check" | jq -er '.command')" || continue
  timeout_seconds="$(printf '%s' "$check" | jq -er '.timeout_seconds // 30')" || timeout_seconds=30

  if output="$(timeout --signal=TERM "${timeout_seconds}s" bash -o pipefail -c "$command_text" </dev/null 2>&1)"; then
    emit_result PASS "$category" "$name" "expected state present"
  else
    rc=$?
    if [[ "$rc" -eq 124 ]]; then
      emit_result FAIL "$category" "$name" "remote check timed out after ${timeout_seconds}s"
    else
      emit_result FAIL "$category" "$name" "remote check exited with status $rc"
    fi
  fi
done < <(printf '%s' "$spec" | jq -c '.checks[]')
"""


@dataclasses.dataclass(frozen=True)
class CheckResult:
    """One validation assertion."""

    status: str
    category: str
    name: str
    detail: str = ""
    host: str = ""

    def as_dict(self) -> dict[str, str]:
        """Return the JSON-serializable result representation."""
        result = {
            "status": self.status,
            "category": self.category,
            "name": self.name,
            "detail": self.detail,
        }
        if self.host:
            result["host"] = self.host
        return result


class AzureCommandError(RuntimeError):
    """Raised when an Azure CLI command fails or returns invalid JSON."""


class AWSCommandError(RuntimeError):
    """Raised when an AWS CLI command fails or returns invalid JSON."""


class AzureCLI:
    """Small JSON-only wrapper around the Azure CLI."""

    def __init__(
        self, executable: str, subscription: str | None, verbose: bool
    ) -> None:
        self.executable = executable
        self.subscription = subscription
        self.verbose = verbose

    def run_json(
        self, args: Sequence[str], *, timeout: int = AZURE_TIMEOUT_SECONDS
    ) -> Any:
        """Run an Azure CLI command and decode its JSON response."""
        command = [self.executable, *args]
        if self.subscription:
            command.extend(["--subscription", self.subscription])
        command.extend(["--only-show-errors", "--output", "json"])
        if self.verbose:
            print(
                f"DEBUG: {shlex.join(redact_azure_command(command))}", file=sys.stderr
            )
        try:
            completed = subprocess.run(
                command,
                check=False,
                capture_output=True,
                text=True,
                timeout=timeout,
            )
        except subprocess.TimeoutExpired as exc:
            raise AzureCommandError(
                f"Azure command timed out after {timeout}s"
            ) from exc
        if completed.returncode != 0:
            detail = (
                completed.stderr.strip()
                or completed.stdout.strip()
                or "unknown Azure CLI error"
            )
            raise AzureCommandError(detail)
        try:
            return json.loads(completed.stdout)
        except json.JSONDecodeError as exc:
            raise AzureCommandError("Azure CLI returned invalid JSON") from exc


class AWSCLI:
    """Small JSON-only wrapper around the AWS CLI."""

    def __init__(self, executable: str, region: str, verbose: bool) -> None:
        self.executable = executable
        self.region = region
        self.verbose = verbose

    def run_json(
        self, args: Sequence[str], *, timeout: int = AWS_TIMEOUT_SECONDS
    ) -> Any:
        """Run an AWS CLI command and decode its JSON response."""
        command = [self.executable, *args, "--region", self.region, "--output", "json"]
        if self.verbose:
            print(f"DEBUG: {shlex.join(redact_aws_command(command))}", file=sys.stderr)
        command_env = os.environ.copy()
        command_env["AWS_PAGER"] = ""
        try:
            completed = subprocess.run(
                command,
                check=False,
                capture_output=True,
                text=True,
                timeout=timeout,
                env=command_env,
            )
        except subprocess.TimeoutExpired as exc:
            raise AWSCommandError(f"AWS command timed out after {timeout}s") from exc
        if completed.returncode != 0:
            detail = (
                completed.stderr.strip()
                or completed.stdout.strip()
                or "unknown AWS CLI error"
            )
            raise AWSCommandError(detail)
        try:
            return json.loads(completed.stdout)
        except json.JSONDecodeError as exc:
            raise AWSCommandError("AWS CLI returned invalid JSON") from exc


def redact_azure_command(command: Sequence[str]) -> list[str]:
    """Redact remote scripts, which contain encoded synthetic credentials."""
    redacted = list(command)
    try:
        script_index = redacted.index("--scripts") + 1
    except ValueError:
        return redacted
    if script_index < len(redacted):
        redacted[script_index] = "<remote-validation-payload>"
    return redacted


def redact_aws_command(command: Sequence[str]) -> list[str]:
    """Redact SSM parameters, which contain encoded synthetic credentials."""
    redacted = list(command)
    try:
        parameters_index = redacted.index("--parameters") + 1
    except ValueError:
        return redacted
    if parameters_index < len(redacted):
        redacted[parameters_index] = "<remote-validation-payload>"
    return redacted


def validate_name_template(
    template: object, label: str, fields: set[str] | None = None
) -> str:
    """Require a name template parameterized by environment and deployment."""
    allowed_fields = fields or {"env", "deployment"}
    if not isinstance(template, str):
        raise ValueError(f"{label} must be a string")
    try:
        parsed = list(string.Formatter().parse(template))
    except ValueError as exc:
        raise ValueError(f"{label} is invalid: {template!r}") from exc
    fields = [item for item in parsed if item[1] is not None]
    if {field_name for _, field_name, _, _ in fields} != allowed_fields or any(
        field_name not in allowed_fields or format_spec or conversion
        for _, field_name, format_spec, conversion in fields
    ):
        raise ValueError(
            f"{label} must contain exactly these replacement fields: {sorted(allowed_fields)}"
        )
    try:
        rendered = template.format(
            env="scope-validation", deployment="goat", host="web01"
        )
    except (AttributeError, IndexError, KeyError, ValueError) as exc:
        raise ValueError(f"{label} is invalid: {template!r}") from exc
    expected_values = {"env": "scope-validation", "deployment": "goat", "host": "web01"}
    if any(expected_values[field] not in rendered for field in allowed_fields):
        raise ValueError(f"{label} must include every declared replacement field")
    return template


def load_manifest(path: pathlib.Path, provider: str = "azure") -> dict[str, Any]:
    """Load and structurally validate the SCOPE-RANGE manifest."""
    try:
        manifest = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"manifest not found: {path}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"manifest is invalid JSON: {exc}") from exc

    if manifest.get("schema_version") != 2:
        raise ValueError("manifest schema_version must be 2")
    if manifest.get("lab") != "SCOPE-RANGE" or provider not in manifest.get(
        "providers", []
    ):
        raise ValueError(f"manifest must describe the {provider} SCOPE-RANGE lab")
    default_environment = manifest.get("default_environment")
    if not isinstance(default_environment, str) or not ENV_NAME_PATTERN.fullmatch(
        default_environment
    ):
        raise ValueError("manifest must contain a valid default_environment")
    deployment_name = manifest.get("deployment_name")
    if not isinstance(deployment_name, str) or not DEPLOYMENT_NAME_PATTERN.fullmatch(
        deployment_name
    ):
        raise ValueError("manifest must contain a valid deployment_name")
    validate_name_template(
        manifest.get("resource_group_template"), "manifest resource_group_template"
    )
    network = manifest.get("network")
    if not isinstance(network, dict) or not REQUIRED_NETWORK_FIELDS.issubset(network):
        raise ValueError("manifest network definition is incomplete")
    if not isinstance(network["expected_public_ip_names"], list):
        raise ValueError("manifest expected_public_ip_names must be a list")
    network_templates = [
        network["vnet_name_template"],
        network["workload_subnet_name_template"],
        network["nat_gateway_name_template"],
        network["nat_public_ip_name_template"],
        *network["expected_public_ip_names"],
    ]
    for index, template in enumerate(network_templates):
        validate_name_template(template, f"manifest network name template {index}")
    aws = manifest.get("aws")
    if not isinstance(aws, dict):
        raise ValueError("manifest AWS definition is incomplete")
    for field in (
        "default_environment",
        "default_region",
        "instance_name_template",
        "vpc_cidr",
        "public_subnets",
        "private_subnets",
        "required_vpc_endpoints",
    ):
        if field not in aws:
            raise ValueError(f"manifest AWS definition requires {field}")
    if not ENV_NAME_PATTERN.fullmatch(str(aws["default_environment"])):
        raise ValueError("manifest AWS default_environment is invalid")
    validate_name_template(
        aws["instance_name_template"],
        "manifest AWS instance_name_template",
        {"env", "deployment", "host"},
    )
    for field in ("public_subnets", "private_subnets", "required_vpc_endpoints"):
        if not isinstance(aws[field], list) or not aws[field]:
            raise ValueError(f"manifest AWS {field} must be a non-empty list")
    hosts = manifest.get("hosts")
    if not isinstance(hosts, list) or not hosts:
        raise ValueError("manifest must contain at least one host")

    seen_hosts: set[str] = set()
    seen_vm_templates: set[str] = set()
    for host in hosts:
        if not isinstance(host, dict):
            raise ValueError("every host entry must be an object")
        host_id = host.get("id")
        vm_template = host.get("vm_name_template")
        if not isinstance(host_id, str) or not host_id:
            raise ValueError("every host must have a non-empty id")
        if host_id in seen_hosts:
            raise ValueError(f"duplicate host id: {host_id}")
        seen_hosts.add(host_id)
        vm_template = validate_name_template(
            vm_template, f"host {host_id} vm_name_template"
        )
        if vm_template in seen_vm_templates:
            raise ValueError(f"duplicate VM template: {vm_template}")
        seen_vm_templates.add(vm_template)
        for field in ("private_ip", "size", "aws_instance_type", "tags"):
            if field not in host:
                raise ValueError(f"host {host_id} requires {field}")
        if not isinstance(host["private_ip"], str) or not host["private_ip"]:
            raise ValueError(f"host {host_id} has invalid private_ip")
        if not isinstance(host["size"], str) or not host["size"]:
            raise ValueError(f"host {host_id} has invalid size")
        if (
            not isinstance(host["aws_instance_type"], str)
            or not host["aws_instance_type"]
        ):
            raise ValueError(f"host {host_id} has invalid aws_instance_type")
        if not isinstance(host["tags"], dict):
            raise ValueError(f"host {host_id} has invalid tags")
        checks = host.get("checks")
        if not isinstance(checks, list) or not checks:
            raise ValueError(f"host {host_id} must define checks")
        seen_checks: set[tuple[str, str]] = set()
        for index, check in enumerate(checks):
            if not isinstance(check, dict):
                raise ValueError(f"host {host_id} check {index} must be an object")
            for field in ("category", "name", "command"):
                if not isinstance(check.get(field), str) or not check[field]:
                    raise ValueError(f"host {host_id} check {index} requires {field}")
            identity = (check["category"], check["name"])
            if identity in seen_checks:
                raise ValueError(
                    f"host {host_id} has duplicate check: {identity[0]}/{identity[1]}"
                )
            seen_checks.add(identity)
            timeout = check.get("timeout_seconds", 30)
            if not isinstance(timeout, int) or not 1 <= timeout <= 300:
                raise ValueError(
                    f"host {host_id} check {index} has invalid timeout_seconds"
                )
            for flag in ("quick", "health"):
                if flag in check and not isinstance(check[flag], bool):
                    raise ValueError(
                        f"host {host_id} check {index} has invalid {flag} flag"
                    )
    return manifest


def resolve_deployment_name(
    manifest: dict[str, Any],
    env: str,
    infra_root: pathlib.Path = DEFAULT_AZURE_INFRA_ROOT,
) -> str:
    """Resolve the resource prefix while retaining legacy environment support."""
    env_file = infra_root / env / "env.hcl"
    try:
        content = env_file.read_text(encoding="utf-8")
    except FileNotFoundError:
        return manifest["deployment_name"]
    matches = DEPLOYMENT_HCL_PATTERN.findall(content)
    if len(matches) != 1:
        raise ValueError(f"{env_file} must define exactly one deployment_name")
    return matches[0]


def render_template(template: str, env: str, deployment: str = "goat") -> str:
    """Render a validated manifest name template."""
    try:
        return template.format(env=env, deployment=deployment)
    except (AttributeError, IndexError, KeyError, ValueError) as exc:
        raise ValueError(f"invalid manifest template {template!r}") from exc


def render_aws_instance_name(
    manifest: dict[str, Any], host_id: str, env: str, deployment: str = "goat"
) -> str:
    """Render the AWS EC2 Name tag for a manifest host."""
    return manifest["aws"]["instance_name_template"].format(
        env=env, deployment=deployment, host=host_id
    )


def result(
    status: str, category: str, name: str, detail: str = "", host: str = ""
) -> CheckResult:
    """Construct a validation result with normalized fields."""
    return CheckResult(
        status=status, category=category, name=name, detail=detail, host=host
    )


def validate_infrastructure(
    azure: AzureCLI,
    manifest: dict[str, Any],
    env: str,
    resource_group: str,
    deployment: str = "goat",
    health: bool = False,
) -> tuple[list[CheckResult], set[str]]:
    """Validate Azure topology and return the VM names eligible for remote checks."""
    results: list[CheckResult] = []
    runnable: set[str] = set()

    try:
        account = azure.run_json(["account", "show"])
        results.append(
            result(
                "PASS",
                "Azure",
                "authenticated Azure subscription",
                f"{account.get('name', 'unknown')} ({account.get('id', 'unknown')})",
            )
        )
    except AzureCommandError as exc:
        results.append(
            result("FAIL", "Azure", "authenticated Azure subscription", str(exc))
        )
        return results, runnable

    try:
        group = azure.run_json(["group", "show", "--name", resource_group])
        state = group.get("properties", {}).get("provisioningState")
        status = "PASS" if state == "Succeeded" else "FAIL"
        results.append(
            result(
                status,
                "Azure",
                "dedicated resource group exists",
                f"{resource_group}: {state}",
            )
        )
    except AzureCommandError as exc:
        results.append(
            result("FAIL", "Azure", "dedicated resource group exists", str(exc))
        )
        return results, runnable

    try:
        vms = azure.run_json(
            ["vm", "list", "--resource-group", resource_group, "--show-details"]
        )
    except AzureCommandError as exc:
        results.append(
            result("FAIL", "Discovery", "enumerate range virtual machines", str(exc))
        )
        return results, runnable

    vm_by_name: dict[str, dict[str, Any]] = {}
    for vm in vms:
        if not isinstance(vm, dict):
            continue
        vm_name = vm.get("name")
        if isinstance(vm_name, str) and vm_name:
            vm_by_name[vm_name] = vm
    expected_names = {
        render_template(host["vm_name_template"], env, deployment)
        for host in manifest["hosts"]
    }
    actual_names = set(vm_by_name)
    unexpected = sorted(actual_names - expected_names)
    missing = sorted(expected_names - actual_names)
    exact_detail = f"expected={len(expected_names)} actual={len(actual_names)}"
    if missing:
        exact_detail += f" missing={','.join(missing)}"
    if unexpected:
        exact_detail += f" unexpected={','.join(unexpected)}"
    results.append(
        result(
            "PASS" if actual_names == expected_names else "FAIL",
            "Discovery",
            "exact VM set",
            exact_detail,
        )
    )

    for host in manifest["hosts"]:
        host_id = host["id"]
        vm_name = render_template(host["vm_name_template"], env, deployment)
        vm = vm_by_name.get(vm_name)
        if vm is None:
            results.append(
                result("FAIL", "Discovery", "virtual machine exists", vm_name, host_id)
            )
            continue
        results.append(
            result("PASS", "Discovery", "virtual machine exists", vm_name, host_id)
        )

        power = vm.get("powerState")
        results.append(
            result(
                "PASS" if power == "VM running" else "FAIL",
                "Discovery",
                "virtual machine is running",
                str(power),
                host_id,
            )
        )
        if power == "VM running":
            runnable.add(vm_name)

        if health:
            continue

        private_ip = vm.get("privateIps")
        results.append(
            result(
                "PASS" if private_ip == host["private_ip"] else "FAIL",
                "Network",
                "private IP matches manifest",
                f"expected={host['private_ip']} actual={private_ip}",
                host_id,
            )
        )
        public_ip = vm.get("publicIps") or ""
        results.append(
            result(
                "PASS" if not public_ip else "FAIL",
                "Network",
                "workload VM has no public IP",
                public_ip or "none",
                host_id,
            )
        )
        size = vm.get("hardwareProfile", {}).get("vmSize")
        results.append(
            result(
                "PASS" if size == host["size"] else "FAIL",
                "Compute",
                "VM size matches manifest",
                f"expected={host['size']} actual={size}",
                host_id,
            )
        )
        actual_tags = vm.get("tags") or {}
        mismatched_tags = [
            f"{key}={actual_tags.get(key)!r}"
            for key, expected in host.get("tags", {}).items()
            if actual_tags.get(key) != expected
        ]
        results.append(
            result(
                "PASS" if not mismatched_tags else "FAIL",
                "Metadata",
                "required Azure tags match manifest",
                "all required tags match"
                if not mismatched_tags
                else ", ".join(mismatched_tags),
                host_id,
            )
        )

    if health:
        return results, runnable

    network = manifest["network"]
    vnet_name = render_template(network["vnet_name_template"], env, deployment)
    workload_nat_gateway = ""
    try:
        vnet = azure.run_json(
            [
                "network",
                "vnet",
                "show",
                "--resource-group",
                resource_group,
                "--name",
                vnet_name,
            ]
        )
        prefixes = vnet.get("addressSpace", {}).get("addressPrefixes", [])
        results.append(
            result(
                "PASS" if network["vnet_cidr"] in prefixes else "FAIL",
                "Network",
                "VNet CIDR matches manifest",
                f"expected={network['vnet_cidr']} actual={','.join(prefixes)}",
            )
        )
        subnet_prefixes = {
            prefix
            for subnet in vnet.get("subnets", [])
            for prefix in (
                subnet.get("addressPrefixes")
                or (
                    [subnet.get("addressPrefix")] if subnet.get("addressPrefix") else []
                )
            )
        }
        expected_subnets = {network["workload_subnet"], network["bastion_subnet"]}
        results.append(
            result(
                "PASS" if expected_subnets.issubset(subnet_prefixes) else "FAIL",
                "Network",
                "workload and Bastion subnets match manifest",
                f"actual={','.join(sorted(subnet_prefixes))}",
            )
        )
        workload_subnet_name = render_template(
            network["workload_subnet_name_template"], env, deployment
        )
        workload_subnet = next(
            (
                subnet
                for subnet in vnet.get("subnets", [])
                if subnet.get("name") == workload_subnet_name
            ),
            {},
        )
        workload_nat_gateway = (
            (workload_subnet.get("natGateway") or {}).get("id", "").rsplit("/", 1)[-1]
        )
    except AzureCommandError as exc:
        results.append(result("FAIL", "Network", "VNet configuration", str(exc)))

    nat_gateway_name = render_template(
        network["nat_gateway_name_template"], env, deployment
    )
    expected_nat_public_ip = render_template(
        network["nat_public_ip_name_template"], env, deployment
    )
    try:
        nat_gateway = azure.run_json(
            [
                "network",
                "nat",
                "gateway",
                "show",
                "--resource-group",
                resource_group,
                "--name",
                nat_gateway_name,
            ]
        )
        nat_public_ips = {
            item.get("id", "").rsplit("/", 1)[-1]
            for item in (nat_gateway.get("publicIpAddresses") or [])
            if item.get("id")
        }
        nat_state = nat_gateway.get("provisioningState")
        nat_valid = (
            workload_nat_gateway == nat_gateway_name
            and nat_state == "Succeeded"
            and nat_public_ips == {expected_nat_public_ip}
        )
        results.append(
            result(
                "PASS" if nat_valid else "FAIL",
                "Network",
                "workload subnet uses the expected NAT gateway",
                f"subnet_gateway={workload_nat_gateway or 'none'} "
                f"state={nat_state} public_ips={','.join(sorted(nat_public_ips)) or 'none'}",
            )
        )
    except AzureCommandError as exc:
        results.append(
            result(
                "FAIL",
                "Network",
                "workload subnet uses the expected NAT gateway",
                str(exc),
            )
        )

    try:
        public_ips = azure.run_json(
            ["network", "public-ip", "list", "--resource-group", resource_group]
        )
        actual_pip_names = {item.get("name") for item in public_ips if item.get("name")}
        expected_pip_names = {
            render_template(template, env, deployment)
            for template in network["expected_public_ip_names"]
        }
        results.append(
            result(
                "PASS" if actual_pip_names == expected_pip_names else "FAIL",
                "Network",
                "only Bastion and NAT public IPs exist",
                f"actual={','.join(sorted(actual_pip_names))}",
            )
        )
    except AzureCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate public IPs", str(exc)))

    bastion_name = f"{env}-{deployment}-bastion"
    try:
        bastion = azure.run_json(
            [
                "network",
                "bastion",
                "show",
                "--resource-group",
                resource_group,
                "--name",
                bastion_name,
            ]
        )
        sku = bastion.get("sku", {}).get("name")
        tunneling = bastion.get("enableTunneling")
        results.append(
            result(
                "PASS" if sku == "Standard" and tunneling is True else "FAIL",
                "Network",
                "Azure Bastion supports tunneling",
                f"sku={sku} tunneling={tunneling}",
            )
        )
    except AzureCommandError as exc:
        results.append(
            result("FAIL", "Network", "Azure Bastion supports tunneling", str(exc))
        )

    return results, runnable


def tags_to_dict(tags: object) -> dict[str, str]:
    """Convert an AWS tag list to a plain mapping."""
    if not isinstance(tags, list):
        return {}
    return {
        str(item.get("Key")): str(item.get("Value"))
        for item in tags
        if isinstance(item, dict) and item.get("Key") is not None
    }


def validate_aws_infrastructure(
    aws: AWSCLI,
    manifest: dict[str, Any],
    env: str,
    deployment: str = "goat",
    health: bool = False,
) -> tuple[list[CheckResult], dict[str, str]]:
    """Validate AWS topology and return remotely runnable host instance IDs."""
    results: list[CheckResult] = []
    runnable: dict[str, str] = {}
    try:
        identity = aws.run_json(["sts", "get-caller-identity"])
        results.append(
            result(
                "PASS",
                "AWS",
                "authenticated AWS account",
                f"{identity.get('Account', 'unknown')} ({identity.get('Arn', 'unknown')})",
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "AWS", "authenticated AWS account", str(exc)))
        return results, runnable

    try:
        response = aws.run_json(
            [
                "ec2",
                "describe-instances",
                "--filters",
                "Name=tag:Project,Values=DreadGOAD",
                f"Name=tag:Environment,Values={env}",
                "Name=instance-state-name,Values=pending,running,stopping,stopped",
            ]
        )
    except AWSCommandError as exc:
        results.append(
            result("FAIL", "Discovery", "enumerate range EC2 instances", str(exc))
        )
        return results, runnable

    instances = [
        instance
        for reservation in response.get("Reservations", [])
        for instance in reservation.get("Instances", [])
        if isinstance(instance, dict)
    ]
    names = [
        tags_to_dict(instance.get("Tags")).get("Name", "") for instance in instances
    ]
    name_counts = collections.Counter(name for name in names if name)
    by_name = {
        name: instance for name, instance in zip(names, instances, strict=True) if name
    }
    expected_names = {
        render_aws_instance_name(manifest, host["id"], env, deployment)
        for host in manifest["hosts"]
    }
    actual_names = set(name_counts)
    exact_instances = (
        actual_names == expected_names
        and len(instances) == len(expected_names)
        and all(count == 1 for count in name_counts.values())
    )
    results.append(
        result(
            "PASS" if exact_instances else "FAIL",
            "Discovery",
            "exact EC2 instance set",
            f"expected={sorted(expected_names)} actual={dict(sorted(name_counts.items()))}",
        )
    )

    running_ids: dict[str, str] = {}
    for host in manifest["hosts"]:
        host_id = host["id"]
        name = render_aws_instance_name(manifest, host_id, env, deployment)
        instance = by_name.get(name)
        if instance is None:
            results.append(
                result("FAIL", "Discovery", "EC2 instance exists", name, host_id)
            )
            continue
        results.append(
            result("PASS", "Discovery", "EC2 instance exists", name, host_id)
        )
        state = instance.get("State", {}).get("Name")
        results.append(
            result(
                "PASS" if state == "running" else "FAIL",
                "Discovery",
                "EC2 instance is running",
                str(state),
                host_id,
            )
        )
        instance_id = str(instance.get("InstanceId", ""))
        if state == "running" and instance_id:
            running_ids[host_id] = instance_id
        if health:
            continue
        private_ip = str(instance.get("PrivateIpAddress", ""))
        results.append(
            result(
                "PASS" if private_ip == host["private_ip"] else "FAIL",
                "Network",
                "private IP matches manifest",
                f"expected={host['private_ip']} actual={private_ip or 'none'}",
                host_id,
            )
        )
        public_ip = str(instance.get("PublicIpAddress", ""))
        results.append(
            result(
                "PASS" if not public_ip else "FAIL",
                "Network",
                "workload instance has no public IP",
                public_ip or "none",
                host_id,
            )
        )
        instance_type = str(instance.get("InstanceType", ""))
        results.append(
            result(
                "PASS" if instance_type == host["aws_instance_type"] else "FAIL",
                "Compute",
                "EC2 instance type matches manifest",
                f"expected={host['aws_instance_type']} actual={instance_type}",
                host_id,
            )
        )
        actual_tags = tags_to_dict(instance.get("Tags"))
        required_tags = {
            **host["tags"],
            "Project": "DreadGOAD",
            "Environment": env,
            "OS": "Linux",
        }
        mismatches = [
            f"{key}={actual_tags.get(key)!r}"
            for key, expected in required_tags.items()
            if actual_tags.get(key) != expected
        ]
        results.append(
            result(
                "PASS" if not mismatches else "FAIL",
                "Metadata",
                "required AWS tags match manifest",
                "all required tags match" if not mismatches else ", ".join(mismatches),
                host_id,
            )
        )

    if running_ids:
        try:
            ssm = aws.run_json(
                [
                    "ssm",
                    "describe-instance-information",
                    "--filters",
                    "Key=InstanceIds,Values=" + ",".join(running_ids.values()),
                ]
            )
            ping = {
                item.get("InstanceId"): item.get("PingStatus")
                for item in ssm.get("InstanceInformationList", [])
            }
            for host_id, instance_id in running_ids.items():
                status = ping.get(instance_id, "NotManaged")
                results.append(
                    result(
                        "PASS" if status == "Online" else "FAIL",
                        "Transport",
                        "SSM agent is online",
                        str(status),
                        host_id,
                    )
                )
                if status == "Online":
                    runnable[host_id] = instance_id
        except AWSCommandError as exc:
            results.append(
                result("FAIL", "Transport", "enumerate SSM agents", str(exc))
            )

    if health:
        return results, runnable

    expected = manifest["aws"]
    try:
        vpcs = aws.run_json(
            [
                "ec2",
                "describe-vpcs",
                "--filters",
                "Name=tag:Project,Values=DreadGOAD",
                "Name=tag:Range,Values=GOAT",
                f"Name=tag:Name,Values={env}-{deployment}",
            ]
        ).get("Vpcs", [])
        exact_vpc = len(vpcs) == 1 and vpcs[0].get("CidrBlock") == expected["vpc_cidr"]
        results.append(
            result(
                "PASS" if exact_vpc else "FAIL",
                "Network",
                "dedicated VPC CIDR matches manifest",
                f"count={len(vpcs)} cidrs={[vpc.get('CidrBlock') for vpc in vpcs]}",
            )
        )
        if len(vpcs) != 1:
            return results, runnable
        vpc_id = vpcs[0]["VpcId"]
    except AWSCommandError as exc:
        results.append(
            result("FAIL", "Network", "dedicated VPC CIDR matches manifest", str(exc))
        )
        return results, runnable

    private_subnet_ids: set[str] = set()
    try:
        subnets = aws.run_json(
            ["ec2", "describe-subnets", "--filters", f"Name=vpc-id,Values={vpc_id}"]
        ).get("Subnets", [])
        actual_public = sorted(
            subnet.get("CidrBlock")
            for subnet in subnets
            if tags_to_dict(subnet.get("Tags")).get("Type") == "public"
        )
        actual_private = sorted(
            subnet.get("CidrBlock")
            for subnet in subnets
            if tags_to_dict(subnet.get("Tags")).get("Type") == "private"
        )
        private_subnet_ids = {
            str(subnet.get("SubnetId"))
            for subnet in subnets
            if tags_to_dict(subnet.get("Tags")).get("Type") == "private"
            and subnet.get("SubnetId")
        }
        valid_subnets = (
            actual_public == sorted(expected["public_subnets"])
            and actual_private == sorted(expected["private_subnets"])
            and all(
                not subnet.get("MapPublicIpOnLaunch")
                for subnet in subnets
                if tags_to_dict(subnet.get("Tags")).get("Type") == "private"
            )
        )
        results.append(
            result(
                "PASS" if valid_subnets else "FAIL",
                "Network",
                "public and private subnets match manifest",
                f"public={actual_public} private={actual_private}",
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate VPC subnets", str(exc)))

    try:
        gateways = aws.run_json(
            [
                "ec2",
                "describe-nat-gateways",
                "--filter",
                f"Name=vpc-id,Values={vpc_id}",
                "Name=state,Values=available",
            ]
        ).get("NatGateways", [])
        results.append(
            result(
                "PASS" if len(gateways) == 1 else "FAIL",
                "Network",
                "exactly one available NAT gateway exists",
                f"count={len(gateways)}",
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate NAT gateways", str(exc)))

    try:
        route_tables = aws.run_json(
            [
                "ec2",
                "describe-route-tables",
                "--filters",
                f"Name=vpc-id,Values={vpc_id}",
            ]
        ).get("RouteTables", [])
        private_tables = [
            table
            for table in route_tables
            if tags_to_dict(table.get("Tags")).get("Name") == f"{env}-{deployment}"
            and any(
                route.get("NatGatewayId")
                and route.get("DestinationCidrBlock") == "0.0.0.0/0"
                for route in table.get("Routes", [])
            )
            and any(
                association.get("SubnetId") in private_subnet_ids
                for association in table.get("Associations", [])
            )
        ]
        results.append(
            result(
                "PASS" if len(private_tables) == 1 else "FAIL",
                "Network",
                "private subnet has the expected NAT default route",
                f"matching_route_tables={len(private_tables)}",
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate VPC routes", str(exc)))

    try:
        endpoints = aws.run_json(
            [
                "ec2",
                "describe-vpc-endpoints",
                "--filters",
                f"Name=vpc-id,Values={vpc_id}",
            ]
        ).get("VpcEndpoints", [])
        actual_services = {
            str(endpoint.get("ServiceName", "")).rsplit(".", 1)[-1]
            for endpoint in endpoints
            if endpoint.get("State") == "available"
        }
        required_services = set(expected["required_vpc_endpoints"])
        results.append(
            result(
                "PASS" if required_services.issubset(actual_services) else "FAIL",
                "Network",
                "required private AWS service endpoints are available",
                f"required={sorted(required_services)} actual={sorted(actual_services)}",
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate VPC endpoints", str(exc)))

    try:
        groups = aws.run_json(
            [
                "ec2",
                "describe-security-groups",
                "--filters",
                f"Name=vpc-id,Values={vpc_id}",
            ]
        ).get("SecurityGroups", [])
        public_ingress = []
        for group in groups:
            for permission in group.get("IpPermissions", []):
                for ip_range in permission.get("IpRanges", []):
                    cidr = ip_range.get("CidrIp", "")
                    if cidr == "0.0.0.0/0":
                        public_ingress.append(str(group.get("GroupId", "unknown")))
                for ip_range in permission.get("Ipv6Ranges", []):
                    if ip_range.get("CidrIpv6") == "::/0":
                        public_ingress.append(str(group.get("GroupId", "unknown")))
        results.append(
            result(
                "PASS" if not public_ingress else "FAIL",
                "Network",
                "security groups expose no public IPv4 ingress",
                "none" if not public_ingress else ",".join(sorted(set(public_ingress))),
            )
        )
    except AWSCommandError as exc:
        results.append(result("FAIL", "Network", "enumerate security groups", str(exc)))

    return results, runnable


def select_host_checks(
    host: dict[str, Any], quick: bool, health: bool = False
) -> dict[str, Any]:
    """Return a copy of one host spec with the requested check subset."""
    if quick and health:
        raise ValueError("quick and health check subsets are mutually exclusive")
    checks = [
        check
        for check in host["checks"]
        if (
            check.get("health") is True
            if health
            else not quick or check.get("quick") is True
        )
    ]
    return {"id": host["id"], "checks": checks}


def build_remote_launcher(
    host: dict[str, Any], quick: bool, health: bool = False
) -> str:
    """Build a shell-safe launcher containing the remote runner and host checks."""
    spec = json.dumps(select_host_checks(host, quick, health), separators=(",", ":"))
    encoded_spec = base64.b64encode(spec.encode("utf-8")).decode("ascii")
    payload = f"SCOPE_VALIDATION_SPEC_B64={shlex.quote(encoded_spec)}\n{REMOTE_RUNNER}"
    encoded_payload = base64.b64encode(payload.encode("utf-8")).decode("ascii")
    return f"printf '%s' {shlex.quote(encoded_payload)} | base64 -d | bash"


def parse_remote_results(message: str, host_id: str) -> list[CheckResult]:
    """Extract structured result lines from Azure Run Command's wrapped message."""
    parsed: list[CheckResult] = []
    for line in message.splitlines():
        if not line.startswith(REMOTE_RESULT_PREFIX):
            continue
        try:
            item = json.loads(line.removeprefix(REMOTE_RESULT_PREFIX))
        except json.JSONDecodeError:
            continue
        if item.get("status") not in {"PASS", "FAIL", "WARN"}:
            continue
        if not isinstance(item.get("category"), str) or not isinstance(
            item.get("name"), str
        ):
            continue
        parsed.append(
            result(
                item["status"],
                item["category"],
                item["name"],
                str(item.get("detail", "")),
                host_id,
            )
        )
    return parsed


def run_host_checks(
    azure: AzureCLI,
    host: dict[str, Any],
    vm_name: str,
    resource_group: str,
    quick: bool,
    health: bool = False,
) -> list[CheckResult]:
    """Execute selected checks in bounded Azure Run Command batches."""
    host_id = host["id"]
    selected = select_host_checks(host, quick, health)["checks"]
    if not selected:
        return [
            result(
                "FAIL",
                "Validation",
                "health checks are defined",
                "manifest selected no checks for this host",
                host_id,
            )
        ]
    results: list[CheckResult] = []

    for offset in range(0, len(selected), REMOTE_BATCH_SIZE):
        batch = selected[offset : offset + REMOTE_BATCH_SIZE]
        launcher = build_remote_launcher({"id": host_id, "checks": batch}, quick=False)
        last_error = "unknown Azure Run Command error"

        for attempt in range(1, 3):
            try:
                response = azure.run_json(
                    [
                        "vm",
                        "run-command",
                        "invoke",
                        "--resource-group",
                        resource_group,
                        "--name",
                        vm_name,
                        "--command-id",
                        "RunShellScript",
                        "--scripts",
                        launcher,
                    ],
                    timeout=AZURE_TIMEOUT_SECONDS,
                )
                values = response.get("value", []) if isinstance(response, dict) else []
                message = (
                    values[0].get("message", "")
                    if values and isinstance(values[0], dict)
                    else ""
                )
                parsed = parse_remote_results(message, host_id)
                results.extend(parsed)
                expected_identities = collections.Counter(
                    (check["category"], check["name"]) for check in batch
                )
                actual_identities = collections.Counter(
                    (check.category, check.name) for check in parsed
                )
                if actual_identities != expected_identities:
                    missing = list((expected_identities - actual_identities).elements())
                    unexpected = list(
                        (actual_identities - expected_identities).elements()
                    )
                    identity_detail = ""
                    if missing:
                        identity_detail += " missing=" + ",".join(
                            f"{category}/{name}" for category, name in missing
                        )
                    if unexpected:
                        identity_detail += " unexpected=" + ",".join(
                            f"{category}/{name}" for category, name in unexpected
                        )
                    results.append(
                        result(
                            "FAIL",
                            "Validation",
                            "remote validation returned every expected result",
                            f"batch={offset // REMOTE_BATCH_SIZE + 1} "
                            f"expected={len(batch)} actual={len(parsed)}"
                            f"{identity_detail}",
                            host_id,
                        )
                    )
                break
            except AzureCommandError as exc:
                last_error = str(exc)
                if attempt < 2:
                    time.sleep(2)
        else:
            results.append(
                result(
                    "FAIL",
                    "Transport",
                    "remote validation command completed",
                    f"batch={offset // REMOTE_BATCH_SIZE + 1}: {last_error}",
                    host_id,
                )
            )

    return results


def run_aws_host_checks(
    aws: AWSCLI,
    host: dict[str, Any],
    instance_id: str,
    quick: bool,
    health: bool = False,
) -> list[CheckResult]:
    """Execute selected checks in bounded AWS SSM shell-command batches."""
    host_id = host["id"]
    selected = select_host_checks(host, quick, health)["checks"]
    if not selected:
        return [
            result(
                "FAIL",
                "Validation",
                "health checks are defined",
                "manifest selected no checks for this host",
                host_id,
            )
        ]
    results: list[CheckResult] = []
    terminal_statuses = {
        "Success",
        "Cancelled",
        "TimedOut",
        "Failed",
        "Cancelling",
    }
    for offset in range(0, len(selected), REMOTE_BATCH_SIZE):
        batch = selected[offset : offset + REMOTE_BATCH_SIZE]
        launcher = build_remote_launcher({"id": host_id, "checks": batch}, quick=False)
        batch_number = offset // REMOTE_BATCH_SIZE + 1
        try:
            response = aws.run_json(
                [
                    "ssm",
                    "send-command",
                    "--instance-ids",
                    instance_id,
                    "--document-name",
                    "AWS-RunShellScript",
                    "--parameters",
                    json.dumps({"commands": [launcher]}, separators=(",", ":")),
                    "--timeout-seconds",
                    str(
                        min(
                            3600,
                            sum(check.get("timeout_seconds", 30) for check in batch)
                            + 60,
                        )
                    ),
                ]
            )
            command_id = response.get("Command", {}).get("CommandId")
            if not command_id:
                raise AWSCommandError("SSM send-command returned no command ID")
            deadline = time.monotonic() + AWS_TIMEOUT_SECONDS
            invocation: dict[str, Any] = {}
            while time.monotonic() < deadline:
                try:
                    current = aws.run_json(
                        [
                            "ssm",
                            "get-command-invocation",
                            "--command-id",
                            str(command_id),
                            "--instance-id",
                            instance_id,
                        ],
                        timeout=60,
                    )
                except AWSCommandError as exc:
                    if "InvocationDoesNotExist" in str(exc):
                        time.sleep(2)
                        continue
                    raise
                if isinstance(current, dict):
                    invocation = current
                if invocation.get("Status") in terminal_statuses:
                    break
                time.sleep(2)
            else:
                raise AWSCommandError("SSM command polling timed out")

            stdout = str(invocation.get("StandardOutputContent", ""))
            parsed = parse_remote_results(stdout, host_id)
            results.extend(parsed)
            expected_identities = collections.Counter(
                (check["category"], check["name"]) for check in batch
            )
            actual_identities = collections.Counter(
                (check.category, check.name) for check in parsed
            )
            if actual_identities != expected_identities:
                detail = (
                    f"batch={batch_number} expected={len(batch)} actual={len(parsed)} "
                    f"ssm_status={invocation.get('Status', 'unknown')}"
                )
                stderr = str(invocation.get("StandardErrorContent", "")).strip()
                if stderr:
                    detail += f" stderr={stderr[:500]}"
                results.append(
                    result(
                        "FAIL",
                        "Validation",
                        "remote validation returned every expected result",
                        detail,
                        host_id,
                    )
                )
        except AWSCommandError as exc:
            results.append(
                result(
                    "FAIL",
                    "Transport",
                    "remote validation command completed",
                    f"batch={batch_number}: {exc}",
                    host_id,
                )
            )
    return results


def run_remote_checks(
    azure: AzureCLI,
    manifest: dict[str, Any],
    env: str,
    resource_group: str,
    runnable: set[str],
    quick: bool,
    deployment: str = "goat",
    health: bool = False,
) -> list[CheckResult]:
    """Run host validation concurrently and return results in manifest order."""
    by_host: dict[str, list[CheckResult]] = {}
    futures: dict[str, concurrent.futures.Future[list[CheckResult]]] = {}
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=len(manifest["hosts"])
    ) as executor:
        for host in manifest["hosts"]:
            vm_name = render_template(host["vm_name_template"], env, deployment)
            if vm_name not in runnable:
                by_host[host["id"]] = [
                    result(
                        "FAIL",
                        "Transport",
                        "remote validation command completed",
                        "VM is missing or not running",
                        host["id"],
                    )
                ]
                continue
            futures[host["id"]] = executor.submit(
                run_host_checks, azure, host, vm_name, resource_group, quick, health
            )
        for host_id, future in futures.items():
            try:
                by_host[host_id] = future.result()
            except Exception as exc:  # noqa: BLE001 - transport isolation is intentional
                by_host[host_id] = [
                    result(
                        "FAIL",
                        "Transport",
                        "remote validation command completed",
                        str(exc),
                        host_id,
                    )
                ]

    ordered: list[CheckResult] = []
    for host in manifest["hosts"]:
        ordered.extend(by_host[host["id"]])
    return ordered


def run_aws_remote_checks(
    aws: AWSCLI,
    manifest: dict[str, Any],
    runnable: dict[str, str],
    quick: bool,
    health: bool = False,
) -> list[CheckResult]:
    """Run AWS host validation concurrently and preserve manifest ordering."""
    by_host: dict[str, list[CheckResult]] = {}
    futures: dict[str, concurrent.futures.Future[list[CheckResult]]] = {}
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=len(manifest["hosts"])
    ) as executor:
        for host in manifest["hosts"]:
            host_id = host["id"]
            instance_id = runnable.get(host_id)
            if not instance_id:
                by_host[host_id] = [
                    result(
                        "FAIL",
                        "Transport",
                        "remote validation command completed",
                        "instance is missing, not running, or not online in SSM",
                        host_id,
                    )
                ]
                continue
            futures[host_id] = executor.submit(
                run_aws_host_checks, aws, host, instance_id, quick, health
            )
        for host_id, future in futures.items():
            try:
                by_host[host_id] = future.result()
            except Exception as exc:  # noqa: BLE001 - transport isolation is intentional
                by_host[host_id] = [
                    result(
                        "FAIL",
                        "Transport",
                        "remote validation command completed",
                        str(exc),
                        host_id,
                    )
                ]
    return [check for host in manifest["hosts"] for check in by_host[host["id"]]]


def print_result(check: CheckResult, color: bool) -> None:
    """Render one result in the GOAD validator's PASS/FAIL/WARN style."""
    symbols = {"PASS": "✓", "FAIL": "✗", "WARN": "⚠"}
    colors = {"PASS": "\033[0;32m", "FAIL": "\033[0;31m", "WARN": "\033[1;33m"}
    prefix = f"{check.host}: " if check.host else ""
    symbol = symbols.get(check.status, "•")
    if color:
        symbol = f"{colors.get(check.status, '')}{symbol}\033[0m"
    print(f"{symbol} {prefix}{check.name}")
    if check.status != "PASS" and check.detail:
        print(f"    {check.detail}")


def build_report(
    results: Sequence[CheckResult],
    env: str,
    resource_group: str,
    subscription: str,
    quick: bool,
    health: bool = False,
    provider_name: str = "azure",
    region: str = "",
) -> dict[str, Any]:
    """Build the stable JSON report consumed by humans and future CLI integration."""
    counts = {
        "total_checks": len(results),
        "passed": sum(item.status == "PASS" for item in results),
        "failed": sum(item.status == "FAIL" for item in results),
        "warnings": sum(item.status == "WARN" for item in results),
    }
    return {
        "schema_version": 1,
        "report_type": "health" if health else "validation",
        "validation_date": dt.datetime.now(dt.timezone.utc)
        .replace(microsecond=0)
        .isoformat(),
        "lab": "SCOPE-RANGE",
        "provider": provider_name,
        "environment": env,
        "resource_group": resource_group if provider_name == "azure" else "",
        "subscription": subscription if provider_name == "azure" else "",
        "aws_account": subscription if provider_name == "aws" else "",
        "region": region,
        "mode": "health" if health else "quick" if quick else "full",
        **counts,
        "checks": [item.as_dict() for item in results],
    }


def write_report(path: pathlib.Path, report: dict[str, Any]) -> None:
    """Atomically write the validation report."""
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temp_name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as stream:
            json.dump(report, stream, indent=2)
            stream.write("\n")
        os.chmod(temp_name, 0o644)
        os.replace(temp_name, path)
    except Exception:
        try:
            os.unlink(temp_name)
        except FileNotFoundError:
            pass
        raise


def build_parser() -> argparse.ArgumentParser:
    """Construct the command-line parser."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--env", default=os.environ.get("ENV"), help="DreadGOAD environment"
    )
    parser.add_argument(
        "--provider",
        choices=("azure", "aws"),
        default=os.environ.get("DREADGOAD_PROVIDER", "azure"),
    )
    parser.add_argument(
        "--deployment-name",
        default=None,
        help="Cloud resource-name component (normally read from the environment HCL)",
    )
    parser.add_argument("--resource-group", default=os.environ.get("RESOURCE_GROUP"))
    parser.add_argument(
        "--subscription", default=os.environ.get("AZURE_SUBSCRIPTION_ID")
    )
    parser.add_argument("--region", default=os.environ.get("AWS_REGION"))
    parser.add_argument(
        "--az-bin",
        default=os.environ.get("AZ_BIN", "az"),
        help=argparse.SUPPRESS,
    )
    parser.add_argument(
        "--aws-bin", default=os.environ.get("AWS_BIN", "aws"), help=argparse.SUPPRESS
    )
    parser.add_argument("--manifest", type=pathlib.Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--output", type=pathlib.Path, default=None)
    subset = parser.add_mutually_exclusive_group()
    subset.add_argument("--quick", action="store_true", help="Run critical checks only")
    subset.add_argument(
        "--health", action="store_true", help="Run core availability checks only"
    )
    parser.add_argument(
        "--json", action="store_true", help="Emit newline-delimited JSON only"
    )
    parser.add_argument("--verbose", action="store_true")
    parser.add_argument(
        "--no-fail", action="store_true", help="Exit zero even when checks fail"
    )
    parser.add_argument(
        "--manifest-only", action="store_true", help="Validate the manifest and exit"
    )
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    """Run SCOPE-RANGE validation and return a process exit status."""
    args = build_parser().parse_args(argv)
    try:
        manifest = load_manifest(args.manifest, args.provider)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    if args.manifest_only:
        print(f"SCOPE-RANGE validation manifest is valid: {args.manifest}")
        return 0

    provider_name = args.provider
    env = args.env or (
        manifest["aws"]["default_environment"]
        if provider_name == "aws"
        else manifest["default_environment"]
    )
    if not ENV_NAME_PATTERN.fullmatch(env):
        print(f"error: invalid environment name: {env!r}", file=sys.stderr)
        return 2
    if args.deployment_name is not None:
        if not DEPLOYMENT_NAME_PATTERN.fullmatch(args.deployment_name):
            print(
                f"error: invalid deployment name: {args.deployment_name!r}",
                file=sys.stderr,
            )
            return 2
        deployment = args.deployment_name
    else:
        try:
            infra_root = (
                DEFAULT_AWS_INFRA_ROOT
                if provider_name == "aws"
                else DEFAULT_AZURE_INFRA_ROOT
            )
            deployment = resolve_deployment_name(manifest, env, infra_root)
        except ValueError as exc:
            print(f"error: {exc}", file=sys.stderr)
            return 2
    resource_group = ""
    region = ""
    if provider_name == "azure":
        resource_group = args.resource_group or render_template(
            manifest["resource_group_template"], env, deployment
        )
        if shutil.which(args.az_bin) is None:
            print(f"error: Azure CLI ({args.az_bin}) is required", file=sys.stderr)
            return 2
    else:
        region = args.region or manifest["aws"]["default_region"]
        if shutil.which(args.aws_bin) is None:
            print(f"error: AWS CLI ({args.aws_bin}) is required", file=sys.stderr)
            return 2

    timestamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    output = args.output
    if output is None and not args.json:
        output = pathlib.Path(f"/tmp/scope-range-validation-{timestamp}.json")
    if not args.json:
        print("==========================================")
        print("SCOPE-RANGE Live Validation")
        print("==========================================")
        print(f"Environment: {env}")
        print(f"Provider: {provider_name}")
        if provider_name == "azure":
            print(f"Resource group: {resource_group}")
        else:
            print(f"Region: {region}")
        print(f"Mode: {'health' if args.health else 'quick' if args.quick else 'full'}")
        print()

    if provider_name == "azure":
        cloud = AzureCLI(args.az_bin, args.subscription, args.verbose)
        infrastructure, runnable_azure = validate_infrastructure(
            cloud, manifest, env, resource_group, deployment, health=args.health
        )
        remote = run_remote_checks(
            cloud,
            manifest,
            env,
            resource_group,
            runnable_azure,
            args.quick,
            deployment,
            args.health,
        )
    else:
        cloud = AWSCLI(args.aws_bin, region, args.verbose)
        infrastructure, runnable_aws = validate_aws_infrastructure(
            cloud, manifest, env, deployment, health=args.health
        )
        remote = run_aws_remote_checks(
            cloud, manifest, runnable_aws, args.quick, args.health
        )
    results = [*infrastructure, *remote]
    color = sys.stdout.isatty() and "NO_COLOR" not in os.environ
    if args.json:
        for check in results:
            item = check.as_dict()
            if args.health:
                item["status"] = {"PASS": "OK", "WARN": "WARN"}.get(
                    check.status, check.status
                )
                if item.get("host") == "kali01":
                    item["host"] = "attackbox"
            print(json.dumps(item, separators=(",", ":")), flush=True)
    else:
        for check in results:
            print_result(check, color)

    try:
        if provider_name == "azure":
            account = cloud.run_json(["account", "show"])
            subscription = str(account.get("id", args.subscription or "unknown"))
        else:
            account = cloud.run_json(["sts", "get-caller-identity"])
            subscription = str(account.get("Account", "unknown"))
    except (AzureCommandError, AWSCommandError):
        subscription = args.subscription or "unknown"
    report = build_report(
        results,
        env,
        resource_group,
        subscription,
        args.quick,
        args.health,
        provider_name,
        region,
    )
    if args.health:
        report["checks"] = [
            {
                **item,
                "status": {"PASS": "OK", "WARN": "WARN"}.get(
                    item["status"], item["status"]
                ),
                **({"host": "attackbox"} if item.get("host") == "kali01" else {}),
            }
            for item in report["checks"]
        ]
        report["skipped"] = 0
    if output is not None:
        try:
            write_report(output, report)
        except OSError as exc:
            print(f"error: could not write report: {exc}", file=sys.stderr)
            return 2

    if args.json:
        print(json.dumps(report, separators=(",", ":")), flush=True)
    else:
        print()
        print("Validation Summary")
        print("------------------")
        print(f"Total: {report['total_checks']}")
        print(f"Passed: {report['passed']}")
        print(f"Failed: {report['failed']}")
        print(f"Warnings: {report['warnings']}")
        if output is not None:
            print(f"Results saved to: {output}")

    if report["failed"] and not args.no_fail:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
