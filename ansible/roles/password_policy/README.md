<!-- DOCSIBLE START -->
# password_policy

## Description

Configure Active Directory password policy settings

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Set password policy** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - password_policy
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
