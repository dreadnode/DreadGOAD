"""Vars plugin that auto-loads the lab JSON config into host variables.

Reads ``data_path`` and ``env`` from the inventory ``[all:vars]`` section,
loads ``{data_path}/{env}-config.json``, and injects the ``lab`` key as a
variable available to all hosts.  This replaces the old pattern of every
playbook importing ``data.yml`` just to call ``include_vars`` + ``set_fact``.
"""

from __future__ import annotations

import json
import os

from ansible.errors import AnsibleParserError
from ansible.inventory.host import Host
from ansible.inventory.group import Group
from ansible.plugins.vars import BaseVarsPlugin
from ansible.utils.display import Display

DOCUMENTATION = """
    name: lab_config
    version_added: "1.0.0"
    short_description: Auto-load lab JSON config from inventory vars
    description:
        - Reads C(data_path) and C(env) from inventory variables and loads
          the corresponding JSON config file.  The top-level C(lab) key from
          the JSON is injected as a variable for all hosts.
    options: {}
"""

display = Display()

# Module-level cache so we only read/parse the JSON once per process,
# regardless of how many hosts or plays trigger get_vars().
_cache: dict | None = None
_cache_path: str | None = None


class VarsModule(BaseVarsPlugin):
    """Inject ``lab`` variable from the environment JSON config file."""

    REQUIRES_ENABLED = True

    def get_vars(self, loader, path, entities, cache=True):
        global _cache, _cache_path  # noqa: PLW0603

        if not entities:
            return {}

        # Resolve data_path and env from inventory variables.
        # We inspect the first entity (host or group) to get access to
        # inventory-level variables.
        data_path = None
        env = None

        for entity in entities:
            if isinstance(entity, Host):
                all_vars = entity.get_vars()
                # Host vars may contain group vars via inheritance
                data_path = all_vars.get("data_path")
                env = all_vars.get("env")
                if data_path and env:
                    break
                # Also check the 'all' group
                for group in entity.get_groups():
                    if group.name == "all":
                        gvars = group.get_vars()
                        data_path = data_path or gvars.get("data_path")
                        env = env or gvars.get("env")
                        break
            elif isinstance(entity, Group):
                gvars = entity.get_vars()
                data_path = gvars.get("data_path")
                env = env or gvars.get("env")

            if data_path and env:
                break

        if not data_path or not env:
            return {}

        # data_path may contain {{ playbook_dir }} which we can't resolve
        # here (no playbook context).  Fall back to resolving relative to
        # the inventory file path.
        if "{{" in str(data_path):
            # The inventory sets data_path relative to playbook_dir, e.g.:
            #   data_path="{{ playbook_dir }}/../../ad/GOAD/data"
            # We can't evaluate Jinja here, but we know the project layout:
            # the inventory file sits at the project root, and the path
            # relative to the project root is ad/<lab>/data.
            # Extract the meaningful suffix after the Jinja expression.
            raw = str(data_path)
            # Strip the Jinja template prefix to get the relative tail
            # e.g. "{{ playbook_dir }}/../../ad/GOAD/data" → "../../ad/GOAD/data"
            parts = raw.split("}}")
            if len(parts) > 1:
                tail = parts[-1].lstrip("/")
            else:
                return {}

            # Resolve from the project root.  Use the inventory source
            # path (passed by Ansible) since the plugin may be installed
            # in ~/.ansible/collections rather than the project tree.
            # The inventory file sits at the project root, so its parent
            # directory IS the project root.
            if os.path.isfile(path):
                project_root = os.path.dirname(os.path.abspath(path))
            elif os.path.isdir(path):
                project_root = os.path.abspath(path)
            else:
                # Fallback: try __file__-based resolution (works when
                # the plugin lives in the project's ansible/plugins/vars/)
                plugin_dir = os.path.dirname(os.path.abspath(__file__))
                project_root = os.path.normpath(
                    os.path.join(plugin_dir, "..", "..", "..", "..")
                )

            # Strip leading "../" segments from the tail since we're
            # resolving from the project root directly.
            while tail.startswith("../"):
                tail = tail[3:]
            data_path = os.path.join(project_root, tail)

        config_file = os.path.join(str(data_path), f"{env}-config.json")

        if not os.path.isfile(config_file):
            # Try without env prefix (some labs use config.json directly)
            config_file_alt = os.path.join(str(data_path), "config.json")
            if os.path.isfile(config_file_alt):
                config_file = config_file_alt
            else:
                display.vvv(f"lab_config: no config file found at {config_file}")
                return {}

        # Return cached result if we already parsed this file
        if cache and _cache is not None and _cache_path == config_file:
            return _cache

        try:
            with open(config_file, "r") as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError) as exc:
            raise AnsibleParserError(
                f"lab_config: failed to load {config_file}: {exc}"
            ) from exc

        result = {}
        if "lab" in data:
            result["lab"] = data["lab"]
        else:
            display.warning(f"lab_config: no 'lab' key in {config_file}")

        if cache:
            _cache = result
            _cache_path = config_file

        display.vvv(f"lab_config: loaded lab config from {config_file}")
        return result
