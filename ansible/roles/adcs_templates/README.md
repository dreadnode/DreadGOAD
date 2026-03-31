<!-- DOCSIBLE START -->
# adcs_templates

## Description

adcs templates

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Refresh** (ansible.windows.win_command)
- **Copy ADCSTemplate zip to remote** (ansible.windows.win_copy)
- **Extract ADCSTemplate module** (ansible.windows.win_shell)
- **Create a template directory** (ansible.windows.win_file)
- **Copy templates json** (ansible.windows.win_copy)
- **Install templates** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - adcs_templates
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
