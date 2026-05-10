<!-- DOCSIBLE START -->
# klink

## Description

Install klink (PuTTY's command-line SSH client) on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `putty_dir` | str | `C:\Program Files\PuTTY` | No description |
| `klink_url` | str | `https://www.9bis.net/kitty/files/klink.exe` | No description |
| `klink_path` | str | `{{ putty_dir }}\klink.exe` | No description |

## Tasks

### main.yml

- **Create PuTTY directory** (ansible.windows.win_file)
- **Check if klink.exe is already installed** (ansible.windows.win_stat)
- **Download klink.exe (only if not present)** (ansible.windows.win_get_url) - Conditional
- **Check klink version** (ansible.windows.win_command)
- **Show klink version** (debug)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - klink
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
