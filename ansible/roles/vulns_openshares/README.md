<!-- DOCSIBLE START -->
# vulns_openshares

## Description

Create open SMB shares for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure directory structure for public share exists** (ansible.windows.win_file)
- **Ensure public share exists** (ansible.windows.win_share)
- **Add or update registry path to allow guest access in SMB** (ansible.windows.win_regedit)
- **Activate guest account** (ansible.windows.win_command)
- **Ensure directory structure for all share exists** (ansible.windows.win_file)
- **Add all share everyone rights** (ansible.windows.win_acl)
- **All shares** (ansible.windows.win_share)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_openshares
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
