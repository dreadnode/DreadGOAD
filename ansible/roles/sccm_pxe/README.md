<!-- DOCSIBLE START -->
# sccm_pxe

## Description

sccm pxe

## Requirements

- Ansible >= 2.15

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `win10_iso_url` | str | `https://software-static.download.prss.microsoft.com/dbazure/988969d5-f34g-4e03-ac9d-1f9786c66750/19045.2006.220908-0225.22h2_release_svc_refresh_CLIENTENTERPRISEEVAL_OEMRET_x64FRE_en-us.iso` | No description |

## Tasks

### main.yml

- **Check downloaded iso file exists** (ansible.windows.win_stat)
- **Check wim file exists** (ansible.windows.win_stat)
- **Download win10 iso file (~ 5.4GB )** (ansible.windows.win_get_url) - Conditional
- **Create share folder** (ansible.windows.win_file)
- **Ensure share exists** (ansible.windows.win_share)
- **Check wim file exists** (ansible.windows.win_stat)
- **Open ISO and extract wim file** (ansible.windows.win_powershell) - Conditional
- **Create Operating system image** (ansible.windows.win_powershell)
- **Create Task sequence** (ansible.windows.win_powershell)
- **Start distribute content** (ansible.windows.win_powershell)
- **Update unknown computers collection** (ansible.windows.win_powershell)
- **Deploy Task sequence** (ansible.windows.win_powershell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - sccm_pxe
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
