<!-- DOCSIBLE START -->
# iis

## Description

Install and configure Internet Information Services web server

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable update service** (ansible.windows.win_service)
- **Check if IIS features are already installed** (ansible.windows.win_powershell)
- **Install all IIS features in single operation** (ansible.windows.win_feature) - Conditional
- **Add SYSTEM allow rights to machine keys** (ansible.windows.win_acl)
- **Create IIS directories** (ansible.windows.win_file)
- **Deploy default website index** (ansible.windows.win_copy)
- **Reboot if required** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - iis
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
