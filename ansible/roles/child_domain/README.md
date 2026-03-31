<!-- DOCSIBLE START -->
# child_domain

## Description

child domain

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable the registration of the NAT interface in DNS** (ansible.windows.win_shell) - Conditional
- **Set configure DNS to parent domain** (ansible.windows.win_dns_client)
- **Install windows features - AD Domain Services** (ansible.windows.win_feature)
- **Install windows features - RSAT-ADDS** (ansible.windows.win_feature)
- **Add child domain to parent domain** (microsoft.ad.domain_child)
- **Configure DNS listener addresses** (ansible.windows.win_powershell) - Conditional
- **Enable TLS 1.2 permanently via registry** (ansible.windows.win_regedit)
- **Check if xDnsServer exists** (ansible.windows.win_shell)
- **Install xDnsServer only if needed** (community.windows.win_psmodule) - Conditional
- **Configure DNS Forwarders** (ansible.windows.win_dsc)
- **Check if ActiveDirectoryDSC exists** (ansible.windows.win_shell)
- **Install ActiveDirectoryDSC only if needed** (community.windows.win_psmodule) - Conditional
- **Enable the Active Directory Web Services** (ansible.windows.win_service)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - child_domain
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
