<!-- DOCSIBLE START -->
# settings_enable_nat_adapter

## Description

Enable the NAT network adapter on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable interface {{ nat_adapter }}** (ansible.windows.win_shell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_enable_nat_adapter
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
