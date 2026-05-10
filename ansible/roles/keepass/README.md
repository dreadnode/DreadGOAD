<!-- DOCSIBLE START -->
# keepass

## Description

Install the KeePass password manager on Windows hosts

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `keepass_url_install_package` | str | `https://unlimited.dl.sourceforge.net/project/keepass/KeePass 2.x/2.60/KeePass-2.60-Setup.exe?viasf=1` | No description |
| `keepass_download_location` | str | `c:\\setup` | No description |
| `keepass_install_bin` | str | `{{keepass_download_location}}\\KeePass-2.60-Setup.exe` | No description |

## Tasks

### main.yml

- **check keepass already exist** (win_stat)
- **Create keepass_download_location folder if not exist** (ansible.windows.win_file) - Conditional
- **Download Keepass to {{keepass_install_bin}}** (ansible.windows.win_get_url) - Conditional
- **Install Keepass** (win_command) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - keepass
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
