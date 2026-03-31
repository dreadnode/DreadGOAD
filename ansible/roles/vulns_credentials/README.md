<!-- DOCSIBLE START -->
# vulns_credentials

## Description

Store credentials in Windows Credential Manager for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Store a password in Credential Manager** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_credentials
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
