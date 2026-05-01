<!-- DOCSIBLE START -->
# common

## Description

Apply common Windows configuration settings for domain-joined hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### chocolatey.yml

- **Ensure chocolatey is installed** (chocolatey.chocolatey.win_chocolatey)
- **Disable enhanced exit codes** (chocolatey.chocolatey.win_chocolatey_feature)
- **Install multiple packages sequentially** (chocolatey.chocolatey.win_chocolatey)

### main.yml

- **Force a DNS on the adapter {{ nat_adapter }}** (ansible.windows.win_dns_client) - Conditional
- **Set a proxy for specific protocols** (ansible.windows.win_inet_proxy) - Conditional
- **Configure IE to use a specific proxy per protocol** (ansible.windows.win_inet_proxy) - Conditional
- **Install DSC modules (skip on prebaked AMIs)** (block) - Conditional
- **Upgrade module PowerShellGet to fix accept license issue** (ansible.windows.win_shell)
- **Check all required modules** (ansible.windows.win_shell)
- **Install all missing modules in parallel** (community.windows.win_psmodule) - Conditional
- **Wait for module installations to complete** (ansible.builtin.async_status) - Conditional
- **Verify DSC LCM is ready** (ansible.windows.win_powershell)
- **Enable RDP (skip on prebaked AMIs)** (block) - Conditional
- **Windows ¦ Enable Remote Desktop** (ansible.windows.win_dsc)
- **Firewall ¦ Allow RDP through Firewall** (ansible.windows.win_dsc)
- **Add a network static route** (ansible.windows.win_route) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - common
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
