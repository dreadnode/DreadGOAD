<!-- DOCSIBLE START -->
# settings_hostname

## Description

Configure Windows hostname and scheduled maintenance tasks

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create scheduled task to keep ssm-user enabled (survives GPO refresh)** (ansible.windows.win_powershell)
- **Change the hostname** (ansible.windows.win_hostname)
- **Reboot if needed** (ansible.windows.win_reboot) - Conditional
- **Ensure ssm-user is enabled after reboot (prevents connection failures)** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_hostname
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
