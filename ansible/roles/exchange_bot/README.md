<!-- DOCSIBLE START -->
# exchange_bot

## Description

Deploy Exchange mailbox bot for automated mail reading

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create setup folder** (ansible.windows.win_file)
- **Copy scripts** (ansible.windows.win_copy)
- **Create schedule task bot_scheduler** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - exchange_bot
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: MIT

## Platforms

<!-- DOCSIBLE END -->
