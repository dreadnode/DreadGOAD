<!-- DOCSIBLE START -->
# vulns_adcs_esc10_case1

## Description

ADCS ESC10 Case 1 - Disable strong certificate binding enforcement

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set StrongCertificateBindingEnforcement to 0** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_esc10_case1
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
