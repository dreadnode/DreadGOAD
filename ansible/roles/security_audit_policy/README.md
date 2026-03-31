<!-- DOCSIBLE START -->
# security_audit_policy

## Description

Configure Windows audit policies for file share and object access logging

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable Detailed File Share auditing (Event 5145)** (ansible.windows.win_shell)
- **Enable File System auditing (Event 4663 - Step 1)** (ansible.windows.win_shell)
- **Enable Handle Manipulation auditing (Event 4658, 4690)** (ansible.windows.win_shell)
- **Configure SACL on SYSVOL folder (Event 4663 - Step 2)** (ansible.windows.win_shell)
- **Configure SACL on NETLOGON folder (Event 4663 - Step 2)** (ansible.windows.win_shell)
- **Configure SACL on additional sensitive folders** (ansible.windows.win_shell) - Conditional
- **Verify audit policies are enabled** (ansible.windows.win_shell)
- **Display audit policy verification** (ansible.builtin.debug)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - security_audit_policy
```

## Author Information

- **Author**: Dreadnode
- **Company**: Dreadnode
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
