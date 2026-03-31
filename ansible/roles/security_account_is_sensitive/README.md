<!-- DOCSIBLE START -->
# security_account_is_sensitive

## Description

Mark Active Directory accounts as sensitive to prevent delegation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Account is sensitive** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - security_account_is_sensitive
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
