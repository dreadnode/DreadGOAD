<!-- DOCSIBLE START -->
# vulns_permissions

## Description

Configure weak file and folder permissions for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Change folder allow rights** (ansible.windows.win_acl)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_permissions
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
