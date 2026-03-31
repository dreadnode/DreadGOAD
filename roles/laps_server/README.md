<!-- DOCSIBLE START -->
# laps_server

## Description

laps server

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### install.yml

- **Create temporary directory for downloads** (ansible.windows.win_file)
- **Download LAPS Package** (ansible.windows.win_get_url)
- **Install to Servers** (ansible.windows.win_package)
- **Reboot after installing LAPS (if required)** (ansible.windows.win_reboot) - Conditional
- **Refresh GPO on the Clients** (ansible.windows.win_shell)

### main.yml

- **Laps server install** (ansible.builtin.import_tasks) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - laps_server
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
