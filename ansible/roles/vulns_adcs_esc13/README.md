<!-- DOCSIBLE START -->
# vulns_adcs_esc13

## Description

ADCS ESC13 - Issuance policy OID group link abuse

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Copy ESC13 script to remote host** (ansible.windows.win_copy)
- **Run ESC13 script** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_esc13
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
