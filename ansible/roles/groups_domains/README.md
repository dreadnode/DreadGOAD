<!-- DOCSIBLE START -->
# groups_domains

## Description

Create and configure Active Directory groups across domains

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Reboot and wait for the AD system to restart** (ansible.windows.win_reboot)
- **Synchronize all domains with proper credentials** (ansible.windows.win_powershell)
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
