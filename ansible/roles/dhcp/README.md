<!-- DOCSIBLE START -->
# dhcp

## Description

Install and configure DHCP server on Windows

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install DHCP** (ansible.windows.win_feature)
- **Reboot if installing windows feature requires it** (ansible.windows.win_reboot) - Conditional
- **Allow dhcp in dc** (ansible.windows.win_shell)
- **Set dhcp scope** (ansible.windows.win_shell)
- **Get default gateway** (ansible.windows.win_shell)
- **Set ip_gateway** (ansible.builtin.set_fact)
- **Add DNS Server and Default Gateway Options in DHCP** (ansible.windows.win_shell)
- **Restart service DHCP** (ansible.windows.win_service)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - dhcp
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
