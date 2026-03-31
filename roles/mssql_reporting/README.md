<!-- DOCSIBLE START -->
# mssql_reporting

## Description

mssql reporting

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory to store the install files** (ansible.windows.win_file)
- **Create directory to store the install files** (ansible.windows.win_file)
- **Reporting Services 2022 exists** (ansible.windows.win_stat)
- **Download SQL Server 2022 Reporting Services** (ansible.windows.win_get_url) - Conditional
- **Install SQL Server 2022 Reporting Services** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - mssql_reporting
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
