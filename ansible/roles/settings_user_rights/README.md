<!-- DOCSIBLE START -->
# settings_user_rights

## Description

Configure user rights assignments for Remote Desktop access

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add remote desktop and administrators group to RDP** (ansible.windows.win_user_right)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_user_rights
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
