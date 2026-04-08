<!-- DOCSIBLE START -->
# fix_dns

## Description

Fix DNS client settings on Windows network adapters

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Change DNS on the adapter {{ nat_adapter }}** (ansible.windows.win_dns_client) - Conditional
- **Change DNS on the adapter {{ nat_adapter }}** (ansible.windows.win_dns_client) - Conditional
- **Change DNS on the adapter {{ nat_adapter }}** (ansible.windows.win_dns_client) - Conditional
- **Change DNS on the adapter {{ nat_adapter }}** (ansible.windows.win_dns_client) - Conditional
- **Change DNS on the adapter {{ domain_adapter }}** (ansible.windows.win_dns_client) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - fix_dns
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
