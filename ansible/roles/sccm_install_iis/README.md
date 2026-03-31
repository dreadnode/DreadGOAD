<!-- DOCSIBLE START -->
# sccm_install_iis

## Description

Install IIS prerequisites for SCCM site server

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory to store the install files** (ansible.windows.win_file)
- **Create directory to store the install files** (ansible.windows.win_file)
- **Install features Remote Differential Compression feature and BITS** (ansible.windows.win_feature)
- **Reboot if installing windows feature requires it** (ansible.windows.win_reboot) - Conditional
- **Enable update service** (ansible.windows.win_service)
- **Install .NET Framework 3.5 with DISM** (ansible.windows.win_shell)
- **Install IIS feature and other components** (ansible.windows.win_feature)
- **Reboot if installing windows feature requires it** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_install_iis
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
