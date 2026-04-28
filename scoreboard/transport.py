"""Transport implementations for reading/deleting the agent's report file."""

import json
import shlex
import subprocess
import time
from abc import ABC, abstractmethod
from pathlib import Path


class Transport(ABC):
    """Abstract base for fetching report.json from the agent's environment."""

    @abstractmethod
    def fetch_report(self) -> str | None:
        """Fetch the raw JSON string of the report file.

        Returns None if the file doesn't exist yet or can't be read.
        """
        ...

    @abstractmethod
    def delete_report(self) -> bool:
        """Delete the report file. Returns True if deleted, False if not found."""
        ...


class LocalTransport(Transport):
    """Read report.json from a local file path."""

    def __init__(self, path: str = "/tmp/report.jsonl"):
        self.path = Path(path)

    def fetch_report(self) -> str | None:
        if not self.path.exists():
            return None
        return self.path.read_text()

    def delete_report(self) -> bool:
        if not self.path.exists():
            return False
        self.path.unlink()
        return True


class SSMTransport(Transport):
    """Read report.json from a remote instance via AWS SSM send-command."""

    def __init__(
        self,
        instance_id: str,
        report_path: str = "/tmp/report.jsonl",
        region: str | None = None,
        profile: str | None = None,
    ):
        self.instance_id = instance_id
        self.report_path = report_path
        self.region = region
        self.profile = profile

    def _build_aws_cmd(self, *args: str) -> list[str]:
        cmd = ["aws"]
        if self.profile:
            cmd.extend(["--profile", self.profile])
        if self.region:
            cmd.extend(["--region", self.region])
        cmd.extend(args)
        return cmd

    def fetch_report(self) -> str | None:
        # Send command to cat the report file
        send_cmd = self._build_aws_cmd(
            "ssm",
            "send-command",
            "--instance-ids",
            self.instance_id,
            "--document-name",
            "AWS-RunShellScript",
            "--parameters",
            json.dumps({"commands": [f"cat {shlex.quote(self.report_path)}"]}),
            "--output",
            "json",
        )

        try:
            result = subprocess.run(
                send_cmd, capture_output=True, text=True, timeout=15
            )
        except subprocess.TimeoutExpired:
            raise ConnectionError(
                "SSM send-command timed out — check network connectivity"
            )

        if result.returncode != 0:
            stderr = result.stderr.strip()
            if "ExpiredTokenException" in stderr or "credentials" in stderr.lower():
                raise ConnectionError(f"AWS credentials expired or invalid: {stderr}")
            if "InvalidInstanceId" in stderr:
                raise ConnectionError(
                    f"Instance {self.instance_id} not found or not SSM-managed"
                )
            raise ConnectionError(
                f"SSM send-command failed: {stderr or f'exit code {result.returncode}'}"
            )

        try:
            command_info = json.loads(result.stdout)
            command_id = command_info["Command"]["CommandId"]
        except (json.JSONDecodeError, KeyError) as exc:
            raise ConnectionError(f"Unexpected SSM response: {exc}")

        # Poll for command output (up to 10 seconds)
        last_err = ""
        for _ in range(10):
            time.sleep(1)
            get_cmd = self._build_aws_cmd(
                "ssm",
                "get-command-invocation",
                "--command-id",
                command_id,
                "--instance-id",
                self.instance_id,
                "--output",
                "json",
            )
            try:
                result = subprocess.run(
                    get_cmd, capture_output=True, text=True, timeout=10
                )
            except subprocess.TimeoutExpired:
                last_err = "get-command-invocation timed out"
                continue

            if result.returncode != 0:
                last_err = result.stderr.strip() or f"exit code {result.returncode}"
                continue

            try:
                invocation = json.loads(result.stdout)
            except json.JSONDecodeError:
                last_err = "malformed JSON from get-command-invocation"
                continue

            status = invocation.get("Status", "")

            if status == "Success":
                output = invocation.get("StandardOutputContent", "").strip()
                return output if output else None
            elif status in ("Failed", "Cancelled", "TimedOut"):
                stderr = invocation.get("StandardErrorContent", "").strip()
                # File not found is not a connectivity error — report doesn't exist yet
                if "No such file" in stderr:
                    return None
                raise ConnectionError(
                    f"SSM command {status.lower()}: {stderr or 'no details'}"
                )

        raise ConnectionError(f"SSM command poll timed out after 10s: {last_err}")

    def delete_report(self) -> bool:
        """Delete the report file on the remote instance via SSM."""
        send_cmd = self._build_aws_cmd(
            "ssm",
            "send-command",
            "--instance-ids",
            self.instance_id,
            "--document-name",
            "AWS-RunShellScript",
            "--parameters",
            json.dumps({"commands": [f"rm -f {shlex.quote(self.report_path)}"]}),
            "--output",
            "json",
        )

        try:
            result = subprocess.run(
                send_cmd, capture_output=True, text=True, timeout=15
            )
        except subprocess.TimeoutExpired:
            raise ConnectionError(
                "SSM send-command timed out — check network connectivity"
            )

        if result.returncode != 0:
            stderr = result.stderr.strip()
            raise ConnectionError(
                f"SSM send-command failed: {stderr or f'exit code {result.returncode}'}"
            )

        return True
