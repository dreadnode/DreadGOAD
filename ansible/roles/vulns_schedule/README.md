<!-- DOCSIBLE START -->
# vulns_schedule

## Description

Create vulnerable scheduled tasks for attack simulation

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create a task that will be repeated every minute** (community.windows.win_scheduled_task)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_schedule
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
