<!-- DOCSIBLE START -->
# vulns_adcs_esc11

## Description

ADCS ESC11 - Disable encrypted certificate request enforcement

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable IF_ENFORCEENCRYPTICERTREQUEST flag (ESC11)** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_esc11
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
