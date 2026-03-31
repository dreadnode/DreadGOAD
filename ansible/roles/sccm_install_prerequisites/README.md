<!-- DOCSIBLE START -->
# sccm_install_prerequisites

## Description

sccm install prerequisites

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create the System Management Container** (ansible.windows.win_powershell)
- **Create the System Management Container** (ansible.windows.win_powershell)
- **Create directory to store the downloaded prerequisite files** (ansible.windows.win_file)
- **Check MECM installation media exists** (ansible.windows.win_stat)
- **Download MECM installation media** (ansible.windows.win_get_url) - Conditional
- **Remove directory if exist** (ansible.windows.win_file)
- **Extract MECM installation media** (ansible.windows.win_shell)
- **Launching the Active Directory schema extension** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_install_prerequisites
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
