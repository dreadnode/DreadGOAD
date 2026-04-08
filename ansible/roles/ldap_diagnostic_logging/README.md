<!-- DOCSIBLE START -->
# ldap_diagnostic_logging

## Description

Configure LDAP diagnostic logging on Domain Controllers for security monitoring

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `ldap_interface_events_level` | int | `2` | No description |
| `field_engineering_level` | int | `5` | No description |
| `ldap_expensive_search_threshold` | int | `1` | No description |
| `ldap_inefficient_search_threshold` | int | `1` | No description |
| `ldap_search_time_threshold` | int | `100` | No description |
| `directory_service_log_max_size_kb` | int | `102400` | No description |
| `ldap_enable_all_diagnostics` | bool | `True` | No description |

## Tasks

### main.yml

- **Enable LDAP Interface Events logging (16 LDAP Interface Events)** (ansible.windows.win_regedit)
- **Enable Field Engineering diagnostics (for Event ID 1644)** (ansible.windows.win_regedit)
- **Set Expensive Search Results Threshold** (ansible.windows.win_regedit)
- **Set Inefficient Search Results Threshold** (ansible.windows.win_regedit)
- **Set Search Time Threshold** (ansible.windows.win_regedit)
- **Enable additional NTDS diagnostics** (ansible.windows.win_regedit) - Conditional
- **Set Directory Service event log maximum size** (ansible.windows.win_shell)
- **Enable Directory Service Access auditing via auditpol** (ansible.windows.win_shell)
- **Create custom event source for LDAP security events** (ansible.windows.win_shell)
- **Ensure Scripts directory exists** (ansible.windows.win_file)
- **Create LDAP monitoring script** (ansible.windows.win_copy)
- **Create scheduled task to monitor LDAP queries** (community.windows.win_scheduled_task)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - ldap_diagnostic_logging
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
