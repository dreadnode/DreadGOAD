<!-- DOCSIBLE START -->
# settings_no_updates

## Description

Disable Windows Update service to preserve lab state

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable windows update** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_no_updates
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
