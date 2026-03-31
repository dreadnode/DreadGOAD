<!-- DOCSIBLE START -->
# sccm_config_client_push

## Description

Configure SCCM client push installation settings

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create Configuration For client push** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_config_client_push
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
