<!-- DOCSIBLE START -->
# gmsa

## Description

Create and configure Group Managed Service Accounts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure KDS root key exists and is effective** (ansible.windows.win_powershell) - Conditional
- **Create GMSA Account** (ansible.windows.win_powershell)
- **Verify GMSA accounts exist** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - gmsa
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
