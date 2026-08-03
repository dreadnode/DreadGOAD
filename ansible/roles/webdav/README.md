<!-- DOCSIBLE START -->
# webdav

## Description

Install and configure WebDAV client on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure WebDAV client feature is installed** (ansible.windows.win_feature)
- **Record pre-reboot boot time baseline (WebDAV install)** (ansible.windows.win_powershell) - Conditional
- **Reboot after installing WebDAV client feature** (block) - Conditional
- **Trigger reboot via win_reboot** (ansible.windows.win_reboot)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - webdav
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
