<!-- DOCSIBLE START -->
# gmsa_hosts

## Description

gmsa hosts

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install-WindowsFeature RSAT-AD-PowerShell** (ansible.windows.win_feature) - Conditional
- **Install ADServiceAccount** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - gmsa_hosts
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
