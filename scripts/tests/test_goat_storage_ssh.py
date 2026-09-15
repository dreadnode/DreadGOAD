"""Regression tests for GOAT storage-host SSH reconciliation."""

from __future__ import annotations

import pathlib
import unittest

import yaml
from jinja2 import Environment


ROOT = pathlib.Path(__file__).resolve().parents[2]
TASKS_PATH = ROOT / "ansible/roles/goat_storage/tasks/main.yml"


class GOATStorageSSHTests(unittest.TestCase):
    """Keep the on-disk SFTP policy and the live daemon in sync."""

    @classmethod
    def setUpClass(cls) -> None:
        cls.tasks = yaml.safe_load(TASKS_PATH.read_text())

    def task(self, name: str) -> tuple[int, dict[str, object]]:
        """Return a uniquely named storage-role task and its position."""
        matches = [
            (index, task)
            for index, task in enumerate(self.tasks)
            if task.get("name") == name
        ]
        self.assertEqual(len(matches), 1, f"expected one task named {name!r}")
        return matches[0]

    def test_reconcile_command_survives_runtime_templating(self) -> None:
        """Catch shell syntax such as ``${#array[@]}`` that Jinja consumes."""
        _, task = self.task("Reconcile the live SSH listener with the SFTP policy")
        shell = task["ansible.builtin.shell"]
        self.assertIsInstance(shell, dict)
        command = shell["cmd"]
        self.assertIsInstance(command, str)
        Environment().from_string(command)

    def test_reconcile_is_immediate_unconditional_and_fail_safe(self) -> None:
        """A retry must reconcile sshd even when blockinfile is unchanged."""
        config_index, config_task = self.task(
            "Enable password authentication only for the synthetic SFTP account"
        )
        reconcile_index, reconcile_task = self.task(
            "Reconcile the live SSH listener with the SFTP policy"
        )

        self.assertEqual(reconcile_index, config_index + 1)
        self.assertNotIn("when", reconcile_task)
        self.assertNotIn("notify", config_task)
        self.assertEqual(
            config_task.get("register"), "goat_sftp_sshd_config"
        )

        shell = reconcile_task["ansible.builtin.shell"]
        self.assertIsInstance(shell, dict)
        command = shell["cmd"]
        self.assertIsInstance(command, str)
        self.assertLess(command.index("/usr/sbin/sshd -t"), command.index("pkill"))
        self.assertIn("pkill -HUP", command)
        self.assertIn("systemctl restart ssh.service", command)


if __name__ == "__main__":
    unittest.main()
