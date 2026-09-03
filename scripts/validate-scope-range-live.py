#!/usr/bin/env python3
"""Validate a deployed Azure SCOPE-RANGE against its expected-state manifest."""

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
ENV_NAME_PATTERN = re.compile(r"^[a-zA-Z0-9][a-zA-Z0-9_-]*$")
REMOTE_RESULT_PREFIX = "SCOPE_RESULT "
AZURE_TIMEOUT_SECONDS = 600
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


def validate_name_template(template: object, label: str) -> str:
    """Require a format template whose only replacement field is ``env``."""
    if not isinstance(template, str):
        raise ValueError(f"{label} must be a string")
    try:
        parsed = list(string.Formatter().parse(template))
    except ValueError as exc:
        raise ValueError(f"{label} is invalid: {template!r}") from exc
    fields = [item for item in parsed if item[1] is not None]
    if not fields or any(
        field_name != "env" or format_spec or conversion
        for _, field_name, format_spec, conversion in fields
    ):
        raise ValueError(f"{label} must contain only the env replacement field")
    try:
        rendered = template.format(env="scope-validation")
    except (AttributeError, IndexError, KeyError, ValueError) as exc:
        raise ValueError(f"{label} is invalid: {template!r}") from exc
    if "scope-validation" not in rendered:
        raise ValueError(f"{label} must include the env replacement field")
    return template


def load_manifest(path: pathlib.Path) -> dict[str, Any]:
    """Load and structurally validate the SCOPE-RANGE manifest."""
    try:
        manifest = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError as exc:
        raise ValueError(f"manifest not found: {path}") from exc
    except json.JSONDecodeError as exc:
        raise ValueError(f"manifest is invalid JSON: {exc}") from exc

    if manifest.get("schema_version") != 1:
        raise ValueError("manifest schema_version must be 1")
    if manifest.get("lab") != "SCOPE-RANGE" or manifest.get("provider") != "azure":
        raise ValueError("manifest must describe the Azure SCOPE-RANGE lab")
    default_environment = manifest.get("default_environment")
    if not isinstance(default_environment, str) or not ENV_NAME_PATTERN.fullmatch(
        default_environment
    ):
        raise ValueError("manifest must contain a valid default_environment")
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
        validate_name_template(vm_template, f"host {host_id} vm_name_template")
        if vm_template in seen_vm_templates:
            raise ValueError(f"duplicate VM template: {vm_template}")
        seen_vm_templates.add(vm_template)
        for field in ("private_ip", "size", "tags"):
            if field not in host:
                raise ValueError(f"host {host_id} requires {field}")
        if not isinstance(host["private_ip"], str) or not host["private_ip"]:
            raise ValueError(f"host {host_id} has invalid private_ip")
        if not isinstance(host["size"], str) or not host["size"]:
            raise ValueError(f"host {host_id} has invalid size")
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
            if "quick" in check and not isinstance(check["quick"], bool):
                raise ValueError(f"host {host_id} check {index} has invalid quick flag")
    return manifest


def render_template(template: str, env: str) -> str:
    """Render a manifest name template with only the validated environment."""
    try:
        return template.format(env=env)
    except (AttributeError, IndexError, KeyError, ValueError) as exc:
        raise ValueError(f"invalid manifest template {template!r}") from exc


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

    vm_by_name = {
        vm.get("name"): vm for vm in vms if isinstance(vm, dict) and vm.get("name")
    }
    expected_names = {
        render_template(host["vm_name_template"], env) for host in manifest["hosts"]
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
        vm_name = render_template(host["vm_name_template"], env)
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

    network = manifest["network"]
    vnet_name = render_template(network["vnet_name_template"], env)
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
            network["workload_subnet_name_template"], env
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

    nat_gateway_name = render_template(network["nat_gateway_name_template"], env)
    expected_nat_public_ip = render_template(
        network["nat_public_ip_name_template"], env
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
            render_template(template, env)
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

    bastion_name = f"{env}-scope-range-bastion"
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


def select_host_checks(host: dict[str, Any], quick: bool) -> dict[str, Any]:
    """Return a copy of one host spec with the requested check subset."""
    checks = [
        check for check in host["checks"] if not quick or check.get("quick") is True
    ]
    return {"id": host["id"], "checks": checks}


def build_remote_launcher(host: dict[str, Any], quick: bool) -> str:
    """Build a shell-safe launcher containing the remote runner and host checks."""
    spec = json.dumps(select_host_checks(host, quick), separators=(",", ":"))
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
) -> list[CheckResult]:
    """Execute selected checks in bounded Azure Run Command batches."""
    host_id = host["id"]
    selected = select_host_checks(host, quick)["checks"]
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


def run_remote_checks(
    azure: AzureCLI,
    manifest: dict[str, Any],
    env: str,
    resource_group: str,
    runnable: set[str],
    quick: bool,
) -> list[CheckResult]:
    """Run host validation concurrently and return results in manifest order."""
    by_host: dict[str, list[CheckResult]] = {}
    futures: dict[str, concurrent.futures.Future[list[CheckResult]]] = {}
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=len(manifest["hosts"])
    ) as executor:
        for host in manifest["hosts"]:
            vm_name = render_template(host["vm_name_template"], env)
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
                run_host_checks, azure, host, vm_name, resource_group, quick
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
) -> dict[str, Any]:
    """Build the stable JSON report consumed by humans and future CLI integration."""
    counts = {
        "total_checks": len(results),
        "passed": sum(item.status == "PASS" for item in results),
        "failed": sum(item.status == "FAIL" for item in results),
        "warnings": sum(item.status == "WARN" for item in results),
    }
    return {
        "validation_date": dt.datetime.now(dt.timezone.utc)
        .replace(microsecond=0)
        .isoformat(),
        "lab": "SCOPE-RANGE",
        "provider": "azure",
        "environment": env,
        "resource_group": resource_group,
        "subscription": subscription,
        "mode": "quick" if quick else "full",
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
    parser.add_argument("--resource-group", default=os.environ.get("RESOURCE_GROUP"))
    parser.add_argument(
        "--subscription", default=os.environ.get("AZURE_SUBSCRIPTION_ID")
    )
    parser.add_argument(
        "--az-bin",
        default=os.environ.get("AZ_BIN", "az"),
        help=argparse.SUPPRESS,
    )
    parser.add_argument("--manifest", type=pathlib.Path, default=DEFAULT_MANIFEST)
    parser.add_argument("--output", type=pathlib.Path, default=None)
    parser.add_argument("--quick", action="store_true", help="Run critical checks only")
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
        manifest = load_manifest(args.manifest)
    except ValueError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 2
    if args.manifest_only:
        print(f"SCOPE-RANGE validation manifest is valid: {args.manifest}")
        return 0

    env = args.env or manifest["default_environment"]
    if not ENV_NAME_PATTERN.fullmatch(env):
        print(f"error: invalid environment name: {env!r}", file=sys.stderr)
        return 2
    resource_group = args.resource_group or render_template(
        manifest["resource_group_template"], env
    )
    if shutil.which(args.az_bin) is None:
        print(f"error: Azure CLI ({args.az_bin}) is required", file=sys.stderr)
        return 2

    timestamp = dt.datetime.now().strftime("%Y%m%d-%H%M%S")
    output = args.output or pathlib.Path(
        f"/tmp/scope-range-validation-{timestamp}.json"
    )
    azure = AzureCLI(args.az_bin, args.subscription, args.verbose)

    print("==========================================")
    print("SCOPE-RANGE Live Validation")
    print("==========================================")
    print(f"Environment: {env}")
    print(f"Resource group: {resource_group}")
    print(f"Mode: {'quick' if args.quick else 'full'}")
    print()

    infrastructure, runnable = validate_infrastructure(
        azure, manifest, env, resource_group
    )
    remote = run_remote_checks(
        azure, manifest, env, resource_group, runnable, args.quick
    )
    results = [*infrastructure, *remote]
    color = sys.stdout.isatty() and "NO_COLOR" not in os.environ
    for check in results:
        print_result(check, color)

    try:
        account = azure.run_json(["account", "show"])
        subscription = str(account.get("id", args.subscription or "unknown"))
    except AzureCommandError:
        subscription = args.subscription or "unknown"
    report = build_report(results, env, resource_group, subscription, args.quick)
    try:
        write_report(output, report)
    except OSError as exc:
        print(f"error: could not write report: {exc}", file=sys.stderr)
        return 2

    print()
    print("Validation Summary")
    print("------------------")
    print(f"Total: {report['total_checks']}")
    print(f"Passed: {report['passed']}")
    print(f"Failed: {report['failed']}")
    print(f"Warnings: {report['warnings']}")
    print(f"Results saved to: {output}")

    if report["failed"] and not args.no_fail:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
