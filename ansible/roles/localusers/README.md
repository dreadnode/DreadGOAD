<!-- DOCSIBLE START -->
# localusers

## Description

Create and manage local Windows user accounts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create local users** (ansible.windows.win_user)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - localusers
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
