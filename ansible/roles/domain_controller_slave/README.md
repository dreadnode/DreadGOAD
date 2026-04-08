<!-- DOCSIBLE START -->
# domain_controller_slave

## Description

Promote a Windows server as a replica domain controller

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set configure dns** (ansible.windows.win_dns_client)
- **Promote the server to additional DC** (microsoft.ad.domain_controller)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - domain_controller_slave
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
