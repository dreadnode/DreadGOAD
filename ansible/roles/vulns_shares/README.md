<!-- DOCSIBLE START -->
# vulns_shares

## Description

Create SMB file shares for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory if not exist** (ansible.windows.win_file)
- **Create share** (ansible.windows.win_share)
- **Set full control permissions** (ansible.builtin.include_tasks) - Conditional
- **Set change permissions** (ansible.builtin.include_tasks) - Conditional
- **Set read permissions** (ansible.builtin.include_tasks) - Conditional
- **Set deny permissions** (ansible.builtin.include_tasks) - Conditional

### perm.yml

- **Add share folder users {{ type }} : {{ perm }} rights** (ansible.windows.win_acl)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_shares
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
