<!-- DOCSIBLE START -->
# sync_domains

## Description

Synchronize Active Directory replication across all domain controllers

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Synchronizes all domains before change schema** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sync_domains
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
