<!-- DOCSIBLE START -->
# logs_windows

## Description

logs windows

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `sysmon_download_url_base` | str | `https://download.sysinternals.com/files` | No description |
| `sysmon_install_location` | str | `C:\sysmon` | No description |
| `sysmon_download_file` | str | `Sysmon` | No description |
| `file_ext` | str | `.zip` | No description |
| `sysmon_config_url` | str | `https://raw.githubusercontent.com/SwiftOnSecurity/sysmon-config/master/sysmonconfig-export.xml` | No description |
| `winlogbeat_service` | dict | `{}` | No description |
| `winlogbeat_service.install_path_64` | str | `C:\Program Files\Elastic\winlogbeat` | No description |
| `winlogbeat_service.install_path_32` | str | `C:\Program Files (x86)\Elastic\winlogbeat` | No description |
| `winlogbeat_service.version` | str | `7.17.6` | No description |
| `winlogbeat_service.download` | bool | `True` | No description |

## Tasks

### main.yml

- **Install winlogbeat** (ansible.builtin.import_tasks)
- **Set winlogbeat config file** (ansible.windows.win_copy)
- **Create directory** (ansible.windows.win_file)
- **Get sysmon zip** (ansible.windows.win_copy)
- **Unzip sysmon** (community.windows.win_unzip)
- **Copy sysmon config** (ansible.windows.win_copy)
- **Check sysmon service** (ansible.windows.win_service)
- **Run sysmon** (ansible.windows.win_command) - Conditional
- **Check winlogbeat service** (ansible.windows.win_service)
- **Reboot before launch setup** (ansible.windows.win_reboot) - Conditional
- **Run winlogbeat setup** (ansible.windows.win_command) - Conditional
- **Check winlogbeat service** (ansible.windows.win_service) - Conditional

### winlogbeat.yml

- **Create 64-bit install directory** (ansible.windows.win_file)
- **Check if winlogbeat service is installed** (ansible.windows.win_service)
- **Check if winlogbeat is using current version** (ansible.windows.win_stat)
- **Copy winlogbeat uninstall script** (ansible.windows.win_copy) - Conditional
- **Uninstall winlogbeat** (ansible.windows.win_shell) - Conditional
- **Download winlogbeat** (ansible.windows.win_get_url) - Conditional
- **Copy winlogbeat** (ansible.windows.win_copy) - Conditional
- **Unzip winlogbeat** (community.windows.win_unzip) - Conditional
- **Configure winlogbeat** (ansible.windows.win_template)
- **Install winlogbeat** (ansible.windows.win_shell) - Conditional
- **Remove other winlogbeat installations** (ansible.windows.win_shell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - logs_windows
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
