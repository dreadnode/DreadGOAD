<!-- DOCSIBLE START -->
# ps

## Description

Execute arbitrary PowerShell scripts on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Play task {{ ps_script }}** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - ps
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
