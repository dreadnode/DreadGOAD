<!-- DOCSIBLE START -->
# sccm_config_users

## Description

Configure SCCM administrative users and permissions

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add full administrators accounts** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_config_users
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
