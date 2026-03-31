<!-- DOCSIBLE START -->
# laps_verify

## Description

laps verify

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Retrieve LAPS Password on server** (ansible.windows.win_shell) - Conditional
- **Show new laps password** (ansible.builtin.debug) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - laps_verify
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
