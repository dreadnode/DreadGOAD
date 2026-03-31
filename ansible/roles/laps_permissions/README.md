<!-- DOCSIBLE START -->
# laps_permissions

## Description

laps permissions

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure AdmPwd.PS module is imported** (ansible.windows.win_shell)
- **Verify LAPS schema attributes exist** (ansible.windows.win_shell)
- **Update LAPS schema if attributes not found** (ansible.windows.win_shell) - Conditional
- **Verify LAPS OU exists** (ansible.windows.win_shell)
- **Add user or group permission to read Laps** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - laps_permissions
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
