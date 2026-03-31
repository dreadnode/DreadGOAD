<!-- DOCSIBLE START -->
# dc_audit_sacl

## Description

Configure SACL auditing on Domain Controllers for attack detection

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `dc_audit_sacl_replication_guids` | list | `[]` | No description |
| `dc_audit_sacl_replication_guids.0` | dict | `{}` | No description |
| `dc_audit_sacl_replication_guids.1` | dict | `{}` | No description |
| `dc_audit_sacl_replication_guids.2` | dict | `{}` | No description |
| `dc_audit_sacl_principal` | str | `S-1-1-0` | No description |
| `dc_audit_sacl_flags` | str | `Success` | No description |
| `dc_audit_sacl_ensure_auditpol` | bool | `True` | No description |
| `dc_audit_sacl_subcategories` | list | `[]` | No description |
| `dc_audit_sacl_subcategories.0` | str | `Directory Service Access` | No description |
| `dc_audit_sacl_subcategories.1` | str | `Directory Service Changes` | No description |

## Tasks

### main.yml

- **Configure auditpol for Directory Service Access** (ansible.windows.win_shell) - Conditional
- **Get current domain DN** (ansible.windows.win_shell)
- **Configure SACL for replication GUIDs (DCSync detection)** (ansible.windows.win_shell)
- **Verify SACL configuration** (ansible.windows.win_shell)
- **Display verification result** (ansible.builtin.debug)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - dc_audit_sacl
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
