<!-- DOCSIBLE START -->
# dc_dns_conditional_forwarder

## Description

dc dns conditional forwarder

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add DNS server zone** (ansible.windows.win_dns_zone) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - dc_dns_conditional_forwarder
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
