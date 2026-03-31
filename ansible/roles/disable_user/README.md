<!-- DOCSIBLE START -->
# disable_user

## Description

Disable an Active Directory user account

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable the user {{ username }}** (ansible.windows.win_user)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - disable_user
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
