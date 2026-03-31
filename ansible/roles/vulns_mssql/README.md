<!-- DOCSIBLE START -->
# vulns_mssql

## Description

Configure vulnerable SQL Server settings for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Run SQL COMMAND** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_mssql
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
