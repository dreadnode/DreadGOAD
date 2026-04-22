<!-- DOCSIBLE START -->
# laps_dc

## Description

Install and configure LAPS on Domain Controllers

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `move_computer` | bool | `False` | No description |
| `prep_servers` | bool | `False` | No description |
| `apply_dacl` | bool | `False` | No description |
| `create_gpo` | bool | `False` | No description |
| `gpo_linked` | bool | `False` | No description |
| `install_servers` | bool | `False` | No description |
| `test_deployment` | bool | `False` | No description |

### Role Variables (main.yml)

| Variable | Type | Value | Description |
| -------- | ---- | ----- | ----------- |
| `pri_laps_password_policy_complexity` | dict | `{}` | No description |
| `pri_laps_password_policy_complexity.uppercase` | int | `1` | No description |
| `pri_laps_password_policy_complexity.uppercase,lowercase` | int | `2` | No description |
| `pri_laps_password_policy_complexity.uppercase,lowercase,digits` | int | `3` | No description |
| `pri_laps_password_policy_complexity.uppercase,lowercase,digits,symbols` | int | `4` | No description |
| `opt_laps_gpo_name` | str | `ansible-laps` | No description |
| `opt_laps_password_policy_complexity` | str | `uppercase,lowercase,digits,symbols` | No description |
| `opt_laps_password_policy_length` | int | `14` | No description |
| `opt_laps_password_policy_age` | int | `30` | No description |

## Tasks

### install.yml

- **Create Laps OU if not exist** (ansible.windows.win_dsc)
- **Create temp directory for downloads** (ansible.windows.win_file)
- **Download LAPS installer to domain controller** (ansible.windows.win_get_url)
- **Install LAPS Package on Servers** (ansible.windows.win_package) - Conditional
- **Reboot After Installing LAPS on Servers** (ansible.windows.win_reboot) - Conditional
- **Configure Password Properties** (dreadnode.goad.win_ad_object)
- **Configure Password Expiry Time** (dreadnode.goad.win_ad_object)
- **Add LAPS attributes to the Computer Attribute** (dreadnode.goad.win_ad_object)
- **Apply DACL to OU Containers** (dreadnode.goad.win_ad_dacl)
- **Create LAPS GPO** (dreadnode.goad.win_gpo)
- **Add LAPS extension to GPO** (dreadnode.goad.win_ad_object)
- **Configure Password Policy Settings on GPO** (dreadnode.goad.win_gpo_reg)
- **Configure Expiration Protection on GPO** (dreadnode.goad.win_gpo_reg)
- **Remove Configuration for Expiration Protection on GPO** (dreadnode.goad.win_gpo_reg)
- **Configure Custom Admin Username Policy on GPO** (dreadnode.goad.win_gpo_reg)
- **Enable the GPO** (dreadnode.goad.win_gpo_reg)
- **Create Comment File for GPO** (ansible.windows.win_copy)
- **Ensure GPO is Linked** (dreadnode.goad.win_gpo_link)

### main.yml

- **Laps dc install** (ansible.builtin.import_tasks) - Conditional
- **Move to laps ou** (ansible.builtin.import_tasks) - Conditional

### move_server_to_ou.yml

- **Move server to Laps OU** (ansible.windows.win_powershell) - Conditional

## Example Playbook

```yaml
- hosts: servers
  roles:
    - laps_dc
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
