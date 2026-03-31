<!-- DOCSIBLE START -->
# commonwkstn

## Description

commonwkstn

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Prioritize the domain interface as the default for routing** (ansible.windows.win_shell) - Conditional
- **Debug the DNS server IP** (ansible.builtin.debug)
- **Set configure dns to {{ dns_domain }}** (ansible.windows.win_dns_client)
- **Add workstation to {{ member_domain }}** (microsoft.ad.membership)
- **Reboot if needed** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - commonwkstn
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
