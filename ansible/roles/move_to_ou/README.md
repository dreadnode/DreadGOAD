<!-- DOCSIBLE START -->
# move_to_ou

## Description

Move computer objects to specified Organizational Units

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Move computer to OU** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - move_to_ou
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
