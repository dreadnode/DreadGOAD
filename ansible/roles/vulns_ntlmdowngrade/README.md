<!-- DOCSIBLE START -->
# vulns_ntlmdowngrade

## Description

Configure NTLM authentication downgrade for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable LmCompatibilityLevel** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_ntlmdowngrade
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
