<!-- DOCSIBLE START -->
# vulns_disable_firewall

## Description

Disable Windows Firewall profiles for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable Domain firewall** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_disable_firewall
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
