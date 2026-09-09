<!-- DOCSIBLE START -->
# groups_domains

## Description

Create and configure Active Directory groups across domains

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Record pre-reboot boot time baseline** (ansible.windows.win_powershell)
- **Reboot and wait for the AD system to restart** (block)
- **Trigger reboot via win_reboot** (ansible.windows.win_reboot)
- **Synchronize all domains with proper credentials** (ansible.windows.win_powershell)
- **Wait for trust secure channel to peer domains** (ansible.windows.win_powershell) - Conditional
- **Add cross-domain users/groups using PowerShell Direct** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - groups_domains
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
