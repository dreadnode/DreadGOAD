<!-- DOCSIBLE START -->
# vulns_no_ldap_signing

## Description

Disable LDAP signing requirement

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable LDAP Signing (RequireSigning = 0)** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_no_ldap_signing
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
