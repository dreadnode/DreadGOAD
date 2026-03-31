<!-- DOCSIBLE START -->
# settings_windows_defender

## Description

Install and configure Windows Defender antivirus settings

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install windows defender** (ansible.windows.win_feature)
- **Reboot if needed** (ansible.windows.win_reboot) - Conditional
- **Disable Windows Defender MAPS cloud reporting** (ansible.windows.win_shell)
- **Disable Windows Defender sample submission consent** (ansible.windows.win_shell)
- **Disable network drive scanning** (ansible.windows.win_shell) - Conditional
- **Disable realtime monitoring** (ansible.windows.win_shell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_windows_defender
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
