<!-- DOCSIBLE START -->
# security_asr

## Description

Configure Windows Defender Attack Surface Reduction rules

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable ASR rule** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - security_asr
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
