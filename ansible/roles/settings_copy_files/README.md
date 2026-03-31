<!-- DOCSIBLE START -->
# settings_copy_files

## Description

settings copy files

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory** (ansible.windows.win_file)
- **Download GOAD img in C:\tmp** (ansible.windows.win_copy)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - settings_copy_files
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
