<!-- DOCSIBLE START -->
# vulns_smbv1

## Description

vulns smuv1

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable SMBV1 feature** (ansible.windows.win_feature)
- **Reboot if feature requires it** (ansible.windows.win_reboot) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_smbv1
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
