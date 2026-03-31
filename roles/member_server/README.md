<!-- DOCSIBLE START -->
# member_server

## Description

memuer server

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Prioritize the domain interface as the default for routing** (ansible.windows.win_shell) - Conditional
- **Set configure DNS to domain controller** (ansible.windows.win_dns_client)
- **Verify File Server Role is installed** (ansible.windows.win_feature)
- **Add member server** (microsoft.ad.membership)
- **Reboot if needed** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - member_server
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
