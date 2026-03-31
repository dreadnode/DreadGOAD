<!-- DOCSIBLE START -->
# vulns_anonymous_enum

## Description

vulns anonymous enum

## Requirements

- Ansible >= 2.15

## Role Variables

## Tasks

### main.yml

- **Enable anonymous enumeration (RestrictAnonymous = 0)** (ansible.windows.win_regedit)
- **Enable anonymous SAM enumeration (RestrictAnonymousSAM = 0)** (ansible.windows.win_regedit)
- **Enable EveryoneIncludesAnonymous** (ansible.windows.win_regedit)
- **Create minimal security policy template with LSAAnonymousNameLookup** (ansible.windows.win_shell)
- **Apply LSAAnonymousNameLookup security policy** (ansible.windows.win_shell)
- **Read secedit log for LSAAnonymousNameLookup apply** (ansible.windows.win_shell)
- **Read scesrv.log tail for LSAAnonymousNameLookup apply** (ansible.windows.win_shell)
- **Remove temporary policy file** (ansible.windows.win_file)
- **Verify LSAAnonymousNameLookup is enabled** (ansible.windows.win_shell)
- **Update Default Domain Controllers Policy if local policy did not apply** (ansible.windows.win_shell) - Conditional
- **Verify LSAAnonymousNameLookup after GPO update** (ansible.windows.win_shell) - Conditional
- **Fail if LSAAnonymousNameLookup not applied** (ansible.builtin.fail) - Conditional
- **Display LSAAnonymousNameLookup verification** (ansible.builtin.debug)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - vulns_anonymous_enum
```

## Author Information

- **Author**: Dreadnode
- **Company**:
- **License**: GPL-3.0-or-later

## Platforms

- Windows: all
<!-- DOCSIBLE END -->
