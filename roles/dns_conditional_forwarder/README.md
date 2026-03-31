<!-- DOCSIBLE START -->
# dns_conditional_forwarder

## Description

dns conditional forwarder

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add DNS server zone** (ansible.windows.win_dns_zone)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - dns_conditional_forwarder
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
