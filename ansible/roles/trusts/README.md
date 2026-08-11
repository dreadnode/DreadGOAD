<!-- DOCSIBLE START -->
# trusts

## Description

Configure Active Directory domain trust relationships

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Prepare to trust flush and renew dns** (ansible.windows.win_shell)
- **Configure forest trust to remote domain** (ansible.windows.win_powershell)
- **Record pre-reboot boot time baseline** (ansible.windows.win_powershell) - Conditional
- **Reboot and wait for the AD system to restart** (block) - Conditional
- **Trigger reboot via win_reboot** (ansible.windows.win_reboot)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - trusts
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
