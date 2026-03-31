<!-- DOCSIBLE START -->
# settings_admin_password

## Description

Set the local administrator account password

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure that Admin is present with a valid password** (ansible.windows.win_user)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_admin_password
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
