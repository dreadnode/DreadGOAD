<!-- DOCSIBLE START -->
# sccm_install_adk

## Description

Install Windows Assessment and Deployment Kit for SCCM

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory to store the install files** (ansible.windows.win_file)
- **Create directory to store the install files** (ansible.windows.win_file)
- **Check ADK version 2004 installation exists** (ansible.windows.win_stat)
- **Download ADK version 2004** (ansible.windows.win_get_url) - Conditional
- **Check ADK adkwinpesetup exists** (ansible.windows.win_stat)
- **Download PE add-on** (ansible.windows.win_get_url) - Conditional
- **Installing ADK version 2004** (ansible.windows.win_shell)
- **Installing PE add-on** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_install_adk
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
