<!-- DOCSIBLE START -->
# vulns_adcs_esc10_case2

## Description

ADCS ESC10 Case 2 - Set UPN certificate mapping method

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set CertificateMappingMethods to 0x4 (UPN)** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_esc10_case2
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
