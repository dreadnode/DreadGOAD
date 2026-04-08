<!-- DOCSIBLE START -->
# wazuh_manager

## Description

Deploy and configure Wazuh manager server

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `wazuh_install_script_url` | str | `https://packages.wazuh.com/4.8/wazuh-install.sh` | No description |
| `socfortress_rules_script_url` | str | `https://raw.githubusercontent.com/socfortress/Wazuh-Rules/main/wazuh_socfortress_rules.sh` | No description |

## Tasks

### main.yml

- **Create /opt/wazuh directory if it does not exist** (ansible.builtin.file)
- **Check services facts** (ansible.builtin.service_facts)
- **Download Wazuh installation script** (ansible.builtin.get_url) - Conditional
- **Run Wazuh installation script** (ansible.builtin.shell) - Conditional
- **Fix rootkit trojan detection due to issue** (ansible.builtin.lineinfile)
- **Start Wazuh Manager service** (ansible.builtin.service)
- **Get stats of ossec directory** (ansible.builtin.stat)
- **Download SOCFORTRESS Wazuh rules script** (ansible.builtin.copy) - Conditional
- **Run SOCFORTRESS Wazuh rules script** (ansible.builtin.shell) - Conditional
- **Extract username and password** (ansible.builtin.shell)
- **Display username and password** (ansible.builtin.debug)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - wazuh_manager
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: MIT

## Platforms

<!-- DOCSIBLE END -->
