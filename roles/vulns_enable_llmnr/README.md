<!-- DOCSIBLE START -->
# vulns_enable_llmnr

## Description

vulns enaule llmnr

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable LLMNR protocol** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_enable_llmnr
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
