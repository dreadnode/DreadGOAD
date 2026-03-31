<!-- DOCSIBLE START -->
# sccm_install_mecm

## Description

Install Microsoft Endpoint Configuration Manager site server

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Create directory to store the downloaded prerequisite files** (ansible.windows.win_file)
- **Download Visual C++ 2017 Redistributable** (ansible.windows.win_get_url)
- **Install Visual C++ 2017 Redistributable** (ansible.windows.win_package) - Conditional
- **Install ODBC Mssql 18 driver** (ansible.windows.win_package)
- **Reboot after installing ODBC if required** (ansible.windows.win_reboot) - Conditional
- **Create directory to store the downloaded prerequisite files** (ansible.windows.win_file)
- **MECM installation media exists** (ansible.windows.win_stat)
- **Download MECM installation media** (ansible.windows.win_get_url) - Conditional
- **Remove directory cd.retail.LN if exist** (ansible.windows.win_file)
- **Extract MECM installation media** (ansible.windows.win_shell)
- **Create directory to store the downloaded prerequisite files** (ansible.windows.win_file)
- **Download prerequisite files** (ansible.windows.win_shell)
- **Copy the configuration file** (ansible.windows.win_template)
- **Fix MSSQL generate certificate issue (change crypto rsa permissions)** (ansible.windows.win_acl)
- **Install MECM (this one take an eternity ~ 1 hour  )** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_install_mecm
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
