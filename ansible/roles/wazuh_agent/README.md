<!-- DOCSIBLE START -->
# wazuh_agent

## Description

Install Wazuh agent on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `wazuh_agent_install_package` | str | `https://packages.wazuh.com/4.x/windows/wazuh-agent-4.8.2-1.msi` | No description |
| `wazuh_install_location` | str | `C:\tmp` | No description |

## Tasks

### main.yml

- **Check if Wazuh Agent service is installed** (ansible.windows.win_service)
- **Create wazuh_install_location folder if not exist** (ansible.windows.win_file)
- **Download Wazuh Agent MSI package** (ansible.windows.win_get_url) - Conditional
- **Install Wazuh Agent** (ansible.windows.win_command) - Conditional
- **Start Wazuh Agent service** (ansible.windows.win_service) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - wazuh_agent
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: MIT

## Platforms

<!-- DOCSIBLE END -->
