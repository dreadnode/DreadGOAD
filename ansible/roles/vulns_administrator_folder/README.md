<!-- DOCSIBLE START -->
# vulns_administrator_folder

## Description

Configure administrator profile folder for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Check if administrator folder already exist** (ansible.windows.win_stat)
- **Check if administrator folder already exist** (ansible.windows.win_stat)
- **Create administrator directory** (ansible.windows.win_file) - Conditional
- **Create administrator desktop directory** (ansible.windows.win_file) - Conditional
- **Disable inherited ACE's** (ansible.windows.win_acl_inheritance) - Conditional
- **Allow C:\users\administrator to administrators** (ansible.windows.win_acl) - Conditional
- **Allow C:\users\administrator to administrators** (ansible.windows.win_acl) - Conditional
- **Allow C:\users\administrator to administrators** (ansible.windows.win_acl) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_administrator_folder
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
