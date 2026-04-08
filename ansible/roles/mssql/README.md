<!-- DOCSIBLE START -->
# mssql

## Description

Install and configure Microsoft SQL Server Express

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `sql_instance_name` | str | `SQLEXPRESS` | No description |
| `sql_version` | str | `MSSQL_2019` | No description |
| `download_url_2019` | str | `https://download.microsoft.com/download/7/f/8/7f8a9c43-8c8a-4f7c-9f92-83c18d96b681/SQL2019-SSEI-Expr.exe` | No description |
| `download_url_2022` | str | `https://download.microsoft.com/download/5/1/4/5145fe04-4d30-4b85-b0d1-39533663a2f1/SQL2022-SSEI-Expr.exe` | No description |
| `connection_type_2019` | str | `-b -E -S localhost\SQLEXPRESS` | No description |
| `connection_type_2022` | str | `-b -S 127.0.0.1,1433` | No description |

## Tasks

### config.yml

- **Ensure BUILTIN\Administrators has SQL sysadmin** (ansible.windows.win_shell)
- **Add MSSQL admin** (ansible.windows.win_shell)
- **Log MSSQL admin errors** (ansible.builtin.debug) - Conditional
- **Add IMPERSONATE on login** (ansible.windows.win_shell)
- **Log IMPERSONATE login errors** (ansible.builtin.debug) - Conditional
- **Add IMPERSONATE on user** (ansible.windows.win_shell)
- **Log IMPERSONATE user errors** (ansible.builtin.debug) - Conditional
- **Enable sa account** (ansible.windows.win_shell)
- **Log sa account errors** (ansible.builtin.debug) - Conditional
- **Enable MSSQL authentication and windows authent** (ansible.windows.win_shell)
- **Enable xp_cmdshell** (ansible.windows.win_shell)
- **Log xp_cmdshell errors** (ansible.builtin.debug) - Conditional
- **Restart service MSSQL** (ansible.windows.win_service) - Conditional

### install.yml

- **Check if reboot is pending before install** (ansible.windows.win_shell)
- **Reboot before install if pending (long timeout in case of update)** (ansible.windows.win_reboot) - Conditional
- **Create SQL Server installation directories** (ansible.windows.win_file)
- **Create and load user profile** (ansible.windows.win_shell)
- **Create SQL Server configuration file** (ansible.windows.win_template)
- **Check if installation media already exists** (ansible.windows.win_stat)
- **Download SQL Server installation media** (ansible.windows.win_get_url) - Conditional
- **Add service account to Log on as a service** (ansible.windows.win_user_right) - Conditional
- **Check if SQL Express media file exists** (ansible.windows.win_stat)
- **Run the installer to download SQL Express installation files** (ansible.windows.win_command) - Conditional
- **Check if MSSQL is installed via registry** (ansible.windows.win_reg_stat)
- **Extract SQL Server installation files** (ansible.windows.win_command) - Conditional
- **Check for lingering SQL Server setup processes** (ansible.windows.win_powershell) - Conditional
- **Install SQL Server** (ansible.windows.win_command) - Conditional
- **Add or update registry for ip port (2022)** (ansible.windows.win_regedit) - Conditional
- **Add or update registry for ip port (2019)** (ansible.windows.win_regedit) - Conditional
- **Reboot if registry was changed** (ansible.windows.win_reboot) - Conditional
- **Firewall ¦ Allow MSSQL through Firewall** (ansible.windows.win_dsc)
- **Firewall ¦ Allow MSSQL discover through Firewall** (ansible.windows.win_dsc)
- **Be sure service is started** (ansible.windows.win_service)
- **Wait for port 1433 to become open on the host, start checking every 5 seconds** (ansible.windows.win_wait_for)

### main.yml

- **Set variables** (ansible.builtin.set_fact)
- **Set service name** (ansible.builtin.set_fact)
- **Display mssql variables in use** (ansible.builtin.debug)
- **Check if SQL Server service exists** (ansible.windows.win_service)
- **Run MSSQL installation tasks** (ansible.builtin.include_tasks) - Conditional
- **Ensure MSSQL service is started** (ansible.windows.win_service) - Conditional
- **Wait for port 1433 to become open** (ansible.windows.win_wait_for)
- **Run MSSQL configuration tasks** (ansible.builtin.include_tasks)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - mssql
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
