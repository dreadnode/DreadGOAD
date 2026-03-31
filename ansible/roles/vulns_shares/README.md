<!-- DOCSIBLE START -->
# vulns_shares

## Description

Create SMB file shares for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory if not exist** (ansible.windows.win_file)
- **Create share** (ansible.windows.win_share)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_shares
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
