<!-- DOCSIBLE START -->
# settings_disable_nat_adapter

## Description

Disable the NAT network adapter on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable interface {{ nat_adapter }}** (ansible.windows.win_shell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_disable_nat_adapter
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
