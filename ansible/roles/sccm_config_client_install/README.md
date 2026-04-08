<!-- DOCSIBLE START -->
# sccm_config_client_install

## Description

Install SCCM client on managed endpoints

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install client** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_config_client_install
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
