<!-- DOCSIBLE START -->
# domain_controller

## Description

domain controller

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable the registration of the NAT interface in DNS** (ansible.windows.win_shell) - Conditional
- **Ensure that domain exists** (microsoft.ad.domain)
- **Check if reboot is pending after domain creation** (ansible.windows.win_powershell) - Conditional
- **Reboot if domain creation requires it** (ansible.windows.win_reboot) - Conditional
- **Re-attempt domain creation** (microsoft.ad.domain) - Conditional
- **Reboot after retry if needed** (ansible.windows.win_reboot) - Conditional
- **Wait for domain to fully initialize** (ansible.windows.win_powershell)
- **Ensure the server is a domain controller** (microsoft.ad.domain_controller)
- **Reboot if domain controller promotion requires it** (ansible.windows.win_reboot) - Conditional
- **Re-attempt domain controller promotion if needed** (microsoft.ad.domain_controller) - Conditional
- **Reboot after DC promotion retry if needed** (ansible.windows.win_reboot) - Conditional
- **Check domain controller status** (ansible.windows.win_powershell)
- **Ensure DNS feature is installed** (ansible.windows.win_feature)
- **Reboot if DNS feature installation requires it** (ansible.windows.win_reboot) - Conditional
- **Check if xDnsServer exists** (ansible.windows.win_shell)
- **Install xDnsServer PowerShell module** (community.windows.win_psmodule) - Conditional
- **Configure DNS listener addresses** (ansible.windows.win_powershell) - Conditional
- **Configure DNS Forwarders** (ansible.windows.win_powershell) - Conditional
- **Check if ActiveDirectoryDSC exists** (ansible.windows.win_shell)
- **Install ActiveDirectoryDSC only if needed** (community.windows.win_psmodule) - Conditional
- **Enable the Active Directory Web Services** (ansible.windows.win_service)
- **Ensure admin groups are properly configured** (block)
- **Ensure admin user is part of Enterprise Admins** (microsoft.ad.group)
- **Ensure admin user is part of Domain Admins** (microsoft.ad.group)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - domain_controller
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
