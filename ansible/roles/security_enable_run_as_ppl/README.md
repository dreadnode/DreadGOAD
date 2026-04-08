<!-- DOCSIBLE START -->
# security_enable_run_as_ppl

## Description

Enable LSA Protection (RunAsPPL) for credential hardening

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable run as PPL** (ansible.windows.win_regedit)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - security_enable_run_as_ppl
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
