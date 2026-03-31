<!-- DOCSIBLE START -->
# sccm_install_wsus

## Description

Install WSUS role as SCCM software update point

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install WSUS** (ansible.windows.win_feature)
- **Reboot and wait for the AD system to restart** (ansible.windows.win_reboot) - Conditional
- **Create directory to store updates** (ansible.windows.win_file)
- **WSUS Post-installation (setup the link with the SQL Server database and a directory to store updates)** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_install_wsus
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
