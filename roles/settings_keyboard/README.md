<!-- DOCSIBLE START -->
# settings_keyboard

## Description

settings keyuoard

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add Keyboard Layouts registry key** (ansible.windows.win_regedit)
- **Add Keyboard Layouts registry key for default users** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_keyboard
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
