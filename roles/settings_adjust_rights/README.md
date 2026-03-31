<!-- DOCSIBLE START -->
# settings_adjust_rights

## Description

settings adjust rights

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add domain users to local groups** (ansible.windows.win_group_membership)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_adjust_rights
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
