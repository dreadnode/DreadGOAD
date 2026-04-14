<!-- DOCSIBLE START -->
# vulns_no_ldap_channel_binding

## Description

Disable LDAPS channel binding requirement

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Disable LDAPS Channel Binding (LdapEnforceChannelBindings = 0)** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_no_ldap_channel_binding
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
