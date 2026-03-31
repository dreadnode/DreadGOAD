<!-- DOCSIBLE START -->
# mssql_audit

## Description

Configure SQL Server auditing for security monitoring

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `mssql_audit_name` | str | `GOAD_SecurityAudit` | No description |
| `mssql_audit_spec_name` | str | `GOAD_AuditSpec` | No description |
| `mssql_audit_file_path` | str | `C:\SQLAudit` | No description |
| `mssql_audit_max_file_size` | int | `100` | No description |
| `mssql_audit_max_rollover_files` | int | `10` | No description |
| `mssql_audit_destination` | str | `APPLICATION_LOG` | No description |
| `mssql_enable_xevents` | bool | `True` | No description |
| `mssql_xevents_session_name` | str | `GOAD_SecurityMonitoring` | No description |
| `mssql_xevents_file_path` | str | `C:\SQLAudit\xevents` | No description |

## Tasks

### main.yml

- **Set MSSQL connection string** (ansible.builtin.set_fact)
- **Create audit directory** (ansible.windows.win_file)
- **Create XEvents directory** (ansible.windows.win_file) - Conditional
- **Enable SQL Server login auditing (all successful and failed logins)** (ansible.windows.win_shell)
- **Create Extended Events session for security monitoring** (ansible.windows.win_shell) - Conditional
- **Start Extended Events session** (ansible.windows.win_shell) - Conditional
- **Create MSSQL SecurityAudit event source** (ansible.windows.win_shell)
- **Create XEvents export script** (ansible.windows.win_copy) - Conditional
- **Create scheduled task to export XEvents to Event Log** (community.windows.win_scheduled_task) - Conditional
- **Restart MSSQL service to apply login audit setting** (ansible.windows.win_service)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - mssql_audit
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: MIT

## Platforms

- Windows: 2019, 2022
<!-- DOCSIBLE END -->
