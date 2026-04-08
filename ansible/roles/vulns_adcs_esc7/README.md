<!-- DOCSIBLE START -->
# vulns_adcs_esc7

## Description

ADCS ESC7 - Grant ManageCA rights for CA officer abuse

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install module PSPKI** (ansible.windows.win_shell)
- **ADD ManageCA rights** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_esc7
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
