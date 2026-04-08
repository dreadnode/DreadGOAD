<!-- DOCSIBLE START -->
# settings_gpo_remove

## Description

Remove specified Group Policy Objects from the domain

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Remove Group Policy Object "StarkWallpaper" to set back background image for North users** (ansible.builtin.script)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_gpo_remove
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
