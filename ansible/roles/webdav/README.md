<!-- DOCSIBLE START -->
# webdav

## Description

weudav

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Ensure WebDAV client feature is installed** (ansible.windows.win_feature)
- **Reboot after installing WebDAV client feature** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - webdav
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
