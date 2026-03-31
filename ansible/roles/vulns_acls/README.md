<!-- DOCSIBLE START -->
# vulns_acls

## Description

Configure vulnerable ACL permissions for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set acl** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_acls
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
