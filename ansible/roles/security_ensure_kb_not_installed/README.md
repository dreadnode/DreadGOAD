<!-- DOCSIBLE START -->
# security_ensure_kb_not_installed

## Description

Ensure specific Windows KB updates are not installed

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Check if KB5008380 is installed** (ansible.windows.win_shell)
- **Remove KB5008380 if installed** (ansible.windows.win_shell) - Conditional
- **Display removal status** (ansible.builtin.debug)
- **Warn if removal failed** (ansible.builtin.debug) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - security_ensure_kb_not_installed
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
