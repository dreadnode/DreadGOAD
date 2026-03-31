<!-- DOCSIBLE START -->
# enable_user

## Description

Enable a disabled Active Directory user account

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable the user {{ username }}** (ansible.windows.win_user)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - enable_user
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
