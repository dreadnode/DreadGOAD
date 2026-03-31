<!-- DOCSIBLE START -->
# acl

## Description

acl

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set ACL for AD objects** (ansible.windows.win_powershell)
- **Wait for ACL operations to complete** (ansible.builtin.async_status) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - acl
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
