<!-- DOCSIBLE START -->
# mssql_ssms

## Description

mssql ssms

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Check if reboot is pending before SSMS install** (ansible.windows.win_powershell)
- **Reboot before SSMS install if pending** (ansible.windows.win_reboot) - Conditional
- **Check SQL Server Manager Studio installer exists** (ansible.windows.win_stat)
- **Get the installer** (ansible.windows.win_get_url) - Conditional
- **Check SSMS installation already done** (ansible.windows.win_powershell)
- **Install SSMS** (ansible.windows.win_command) - Conditional
- **Reboot after install** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - mssql_ssms
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
