<!-- DOCSIBLE START -->
# vulns_adcs_esc7

## Description

ADCS ESC7 - Grant ManageCA rights for CA officer abuse

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure NuGet provider is installed** (ansible.windows.win_shell)
- **Install module PSPKI** (ansible.windows.win_powershell)
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
