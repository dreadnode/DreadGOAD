<!-- DOCSIBLE START -->
# vulns_adcs_templates

## Description

vulns adcs templates

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Refresh** (ansible.windows.win_command)
- **Copy ADCSTemplate zip to remote** (ansible.windows.win_copy)
- **Extract ADCSTemplate module** (ansible.windows.win_shell)
- **Create a directory for templates** (ansible.windows.win_file)
- **Install templates** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_adcs_templates
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
