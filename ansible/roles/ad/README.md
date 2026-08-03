<!-- DOCSIBLE START -->
# ad

## Description

Configure Active Directory domain administrator membership and settings

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `ad_reconcile_passwords` | bool | `True` | No description |
| `ad_reconcile_group_membership` | bool | `True` | No description |
| `ad_reconcile_check_only` | bool | `False` | No description |
| `ad_reconcile_protected_accounts` | list | `[]` | No description |
| `ad_reconcile_protected_accounts.0` | str | `ssm-user` | No description |
| `ad_reconcile_protected_accounts.1` | str | `ansible` | No description |
| `ad_reconcile_protected_accounts.2` | str | `vagrant` | No description |
| `ad_multi_domain_groups_member` | dict | `{}` | No description |

## Tasks

### groups.yml

- **Create Universal Groups** (microsoft.ad.group) - Conditional
- **Create Global Groups** (microsoft.ad.group) - Conditional
- **Create DomainLocal Groups** (microsoft.ad.group) - Conditional

### main.yml

- **Ensure Administrator is part of Domain Admins** (ansible.windows.win_powershell)
- **Organisation units** (ansible.builtin.import_tasks)
- **Groups** (ansible.builtin.import_tasks)
- **Users** (ansible.builtin.import_tasks)
- **Reconcile user passwords** (ansible.builtin.import_tasks) - Conditional
- **Add members to the Domainlocal group, preserving existing membership** (microsoft.ad.group) - Conditional
- **Add members to the Universal group, preserving existing membership** (microsoft.ad.group) - Conditional
- **Add members to the Global group, preserving existing membership** (microsoft.ad.group) - Conditional
- **Reconcile group membership** (ansible.builtin.import_tasks) - Conditional
- **Assign managed_by domainlocal groups** (ansible.windows.win_powershell) - Conditional
- **Assign managed_by universal groups** (ansible.windows.win_powershell) - Conditional
- **Assign managed_by global groups** (ansible.windows.win_powershell) - Conditional

### ou.yml

- **Create OU** (ansible.windows.win_powershell)
- **Wait for OU creation to complete** (ansible.builtin.async_status) - Conditional

### reconcile_group_membership.yml

- **Reconcile group membership against the lab config** (ansible.windows.win_powershell)

### reconcile_passwords.yml

- **Confirm the credential probe rejects invalid credentials** (ansible.windows.win_powershell)
- **Reconcile user passwords against the lab config** (ansible.windows.win_powershell)
- **Report password drift** (ansible.builtin.debug) - Conditional

### users.yml

- **Sync the contents of one directory to another - hack to get Requires -Module Ansible.ModuleUtils.Legacy loaded** (community.windows.win_robocopy)
- **Create users** (ansible.windows.win_powershell)
- **Wait for user creation to complete** (ansible.builtin.async_status) - Conditional
- **Set users SPN lists** (ansible.windows.win_powershell) - Conditional
- **Wait for SPN configuration to complete** (ansible.builtin.async_status) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - ad
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
