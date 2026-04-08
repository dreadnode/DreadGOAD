<!-- DOCSIBLE START -->
# mssql_link

## Description

Configure linked servers between SQL Server instances

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### logins.yml

- **Ensure SQL Server is in multi-user mode before creating logins** (ansible.windows.win_powershell)
- **Configure logins mapping to specific users** (ansible.windows.win_powershell)

### main.yml

- **Set SqlCmd path** (ansible.windows.win_shell)
- **Create SQL Linked server and enable RPC** (ansible.windows.win_powershell)
- **Create logins** (ansible.builtin.include_tasks)
- **Default login impersonation** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - mssql_link
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
