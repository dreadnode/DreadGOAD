<!-- DOCSIBLE START -->
# settings_updates

## Description

Install Windows updates on managed hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### default.yml

- **Enable update service** (ansible.windows.win_service)
- **Install all updates and reboot as many times as needed** (ansible.windows.win_updates)

### main.yml

- **Enable update service** (ansible.windows.win_service)
- **Install all updates and reboot as many times as needed** (ansible.windows.win_updates)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_updates
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
