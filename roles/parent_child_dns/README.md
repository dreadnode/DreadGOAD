<!-- DOCSIBLE START -->
# parent_child_dns

## Description

parent child dns

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Add dns delegation to child domain** (ansible.windows.win_shell) - Conditional
- **Create conditional forwarder to child domain** (ansible.windows.win_dns_zone) - Conditional
- **Debug IP resolution for child domains** (ansible.builtin.debug) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - parent_child_dns
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
