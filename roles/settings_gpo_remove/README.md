<!-- DOCSIBLE START -->
# settings_gpo_remove

## Description

settings gpo remove

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
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
