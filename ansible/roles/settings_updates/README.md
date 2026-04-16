<!-- DOCSIBLE START -->
# settings_updates

## Description

Install Windows updates on managed hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Run Windows Updates (skip on prebaked AMIs)** (block) - Conditional
- **Prevent forced user registry unload (fixes WUA 0x800703FA)** (ansible.windows.win_regedit)
- **Reset Windows Update components** (ansible.windows.win_shell)
- **Reboot to clear pending registry operations** (ansible.windows.win_reboot)
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
