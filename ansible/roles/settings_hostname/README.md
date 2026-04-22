<!-- DOCSIBLE START -->
# settings_hostname

## Description

Configure Windows hostname and scheduled maintenance tasks

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Check current hostname and domain state** (ansible.windows.win_powershell)
- **Remove ADCS features before DC demotion** (ansible.windows.win_feature) - Conditional
- **Reboot after ADCS removal (required before demotion)** (ansible.windows.win_reboot) - Conditional
- **Demote domain controller before hostname change** (microsoft.ad.domain_controller) - Conditional
- **Reboot after DC demotion** (ansible.windows.win_reboot) - Conditional
- **Unjoin from domain before hostname change** (microsoft.ad.membership) - Conditional
- **Reboot after domain unjoin** (ansible.windows.win_reboot) - Conditional
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
