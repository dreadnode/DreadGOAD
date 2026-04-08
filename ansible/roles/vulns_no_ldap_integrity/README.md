<!-- DOCSIBLE START -->
# vulns_no_ldap_integrity

## Description

Disable LDAP server integrity requirement

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable LDAP Server Integrity (LDAPServerIntegrity = 0)** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_no_ldap_integrity
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
