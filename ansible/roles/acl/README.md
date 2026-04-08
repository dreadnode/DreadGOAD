<!-- DOCSIBLE START -->
# acl

## Description

Configure Active Directory ACL permissions on objects

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set ACL for AD objects** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - acl
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
