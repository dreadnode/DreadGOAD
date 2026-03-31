<!-- DOCSIBLE START -->
# adcs

## Description

adcs

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Install ADCS** (ansible.windows.win_feature)
- **Install-WindowsFeature ADCS-Cert-Authority** (ansible.windows.win_feature)
- **Install-WindowsFeature ADCS-Web-Enrollment** (ansible.windows.win_feature) - Conditional
- **Install-ADCSCertificationAuthority-PS** (ansible.windows.win_powershell)
- **Enable Web enrollement** (ansible.windows.win_powershell) - Conditional
- **Refresh Group Policy** (ansible.windows.win_shell)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - adcs
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
