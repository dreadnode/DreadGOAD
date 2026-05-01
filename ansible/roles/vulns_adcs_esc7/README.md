<!-- DOCSIBLE START -->
# vulns_adcs_esc7

## Description

ADCS ESC7 - Grant ManageCA rights for CA officer abuse

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Read installed .NET Framework release key** (ansible.windows.win_reg_stat)
- **Install .NET Framework 4.8 (PSPKI 4.x requires >=4.7.2)** (chocolatey.chocolatey.win_chocolatey) - Conditional
- **Reboot to complete .NET Framework upgrade** (ansible.windows.win_reboot) - Conditional
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
