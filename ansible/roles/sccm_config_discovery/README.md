<!-- DOCSIBLE START -->
# sccm_config_discovery

## Description

Configure SCCM Active Directory discovery methods

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Restart service SMS_SITE_COMPONENT_MANAGER** (ansible.windows.win_service)
- **Setup discovery** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_config_discovery
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
