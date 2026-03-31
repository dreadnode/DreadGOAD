<!-- DOCSIBLE START -->
# vulns_disable_firewall

## Description

vulns disaule firewall

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable Domain firewall** (ansible.windows.win_firewall)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_disable_firewall
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
