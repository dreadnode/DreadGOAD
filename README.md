# dreadnode.goad

Ansible collection for deploying and configuring vulnerable Active Directory
lab environments for penetration testing and security research.

Based on [GOAD (Game of Active Directory)](https://github.com/Orange-Cyberdefense/GOAD)
by Orange Cyberdefense.

---

## Architecture Diagram

```mermaid
graph TD
    Collection[dreadnode.goad]

    Collection --> AD[Active Directory]
    Collection --> Server[Server Roles]
    Collection --> LAPS[LAPS]
    Collection --> Vulns[Vulnerabilities]
    Collection --> SCCM[SCCM]
    Collection --> Security[Security]
    Collection --> Settings[Settings]
    Collection --> Playbooks[Playbooks]

    AD --> ad & acl & adcs & adcs_templates
    AD --> domain_controller & domain_controller_slave
    AD --> child_domain & member_server & trusts
    AD --> gmsa & gmsa_hosts & password_policy
    AD --> move_to_ou & groups_domains & onlyusers
    AD --> dns_conditional_forwarder & dc_dns_conditional_forwarder
    AD --> parent_child_dns & sync_domains
    AD --> disable_user & enable_user

    Server --> common & commonwkstn & localusers
    Server --> mssql & mssql_link & mssql_ssms & mssql_reporting & mssql_audit
    Server --> iis & elk & webdav & dhcp
    Server --> logs_windows & ldap_diagnostic_logging
    Server --> fix_dns & ps

    LAPS --> laps_dc & laps_server & laps_permissions & laps_verify

    Vulns --> vulns_credentials & vulns_acls & vulns_permissions
    Vulns --> vulns_shares & vulns_openshares & vulns_files & vulns_directory
    Vulns --> vulns_smbv1 & vulns_disable_firewall & vulns_anonymous_enum
    Vulns --> vulns_autologon & vulns_ntlmdowngrade & vulns_schedule
    Vulns --> vulns_mssql & vulns_adcs_templates & vulns_administrator_folder
    Vulns --> vulns_enable_llmnr & vulns_enable_nbt_ns
    Vulns --> vulns_enable_credssp_server & vulns_enable_credssp_client

    SCCM --> sccm_install_prerequisites & sccm_install_adk & sccm_install_mecm
    SCCM --> sccm_install_wsus & sccm_install_iis & sccm_pxe
    SCCM --> sccm_config_accounts & sccm_config_boundary & sccm_config_discovery
    SCCM --> sccm_config_client_push & sccm_config_client_install
    SCCM --> sccm_config_naa & sccm_config_pxe & sccm_config_users

    Security --> security_audit_policy & security_asr
    Security --> security_enable_run_as_ppl & security_account_is_sensitive
    Security --> security_powershell_restrict & security_ensure_kb_not_installed
    Security --> dc_audit_sacl

    Settings --> settings_hostname & settings_keyboard & settings_admin_password
    Settings --> settings_updates & settings_no_updates & settings_windows_defender
    Settings --> settings_copy_files & settings_user_rights & settings_adjust_rights
    Settings --> settings_enable_nat_adapter & settings_disable_nat_adapter
    Settings --> settings_gpo_remove & settings_gpmc

    Playbooks --> base
```

## Requirements

- Ansible >= 2.15
- Windows target hosts accessible via WinRM or AWS SSM

### Collection Dependencies

- `ansible.windows` >= 2.5.0
- `community.general`
- `community.windows` >= 2.3.0
- `chocolatey.chocolatey` >= 1.5.3
- `microsoft.ad`

---

## Installation

### From source

```bash
ansible-galaxy collection build .
ansible-galaxy collection install dreadnode-goad-1.0.0.tar.gz
```

### Install dependencies

```bash
ansible-galaxy collection install -r ansible/requirements.yml
```

---

## Lab Environment

The GOAD lab provides:

- **3 domains**: `sevenkingdoms.local`, `north.sevenkingdoms.local`,
  `essos.local`
- **2 forests** with cross-domain trusts
- **5-6 hosts**: Domain controllers + member servers (Windows Server 2016/2019)

---

## Roles

### Active Directory

| Role | Description |
| ---- | ----------- |
| `domain_controller` | Promote server to domain controller |
| `domain_controller_slave` | Add replica domain controller |
| `child_domain` | Create child domain |
| `member_server` | Join server to domain |
| `ad` | Create AD users, groups, and OUs |
| `acl` | Configure AD ACLs and permissions |
| `adcs` | Install Active Directory Certificate Services |
| `adcs_templates` | Deploy ADCS certificate templates |
| `trusts` | Configure cross-domain trusts |
| `gmsa` | Configure group managed service accounts |
| `gmsa_hosts` | Configure gMSA host permissions |
| `password_policy` | Set domain password policies |
| `move_to_ou` | Move objects to organizational units |
| `groups_domains` | Configure cross-domain group membership |
| `dns_conditional_forwarder` | Configure DNS conditional forwarders |
| `dc_dns_conditional_forwarder` | Configure DC-specific DNS forwarders |
| `parent_child_dns` | Configure parent-child domain DNS |
| `sync_domains` | Synchronize domain data |
| `onlyusers` | Create AD users only |
| `disable_user` | Disable AD user accounts |
| `enable_user` | Enable AD user accounts |

### Server Roles

| Role | Description |
| ---- | ----------- |
| `common` | Base server configuration (DNS, proxy, modules) |
| `commonwkstn` | Workstation-specific configuration |
| `iis` | Install and configure IIS |
| `mssql` | Install and configure SQL Server |
| `mssql_link` | Configure SQL Server linked servers |
| `mssql_ssms` | Install SQL Server Management Studio |
| `mssql_reporting` | Install SQL Server Reporting Services |
| `mssql_audit` | Configure SQL Server audit logging |
| `elk` | Install Elasticsearch, Logstash, Kibana |
| `logs_windows` | Configure Windows event logging |
| `webdav` | Configure WebDAV server |
| `dhcp` | Configure DHCP server |
| `localusers` | Manage local user accounts |
| `fix_dns` | Fix DNS configuration issues |
| `ps` | Execute PowerShell scripts |

### LAPS

| Role | Description |
| ---- | ----------- |
| `laps_dc` | Install LAPS on domain controllers |
| `laps_server` | Install LAPS on member servers |
| `laps_verify` | Verify LAPS installation |
| `laps_permissions` | Configure LAPS permissions |

### Settings

| Role | Description |
| ---- | ----------- |
| `settings_hostname` | Set Windows hostname |
| `settings_admin_password` | Set local admin password |
| `settings_keyboard` | Configure keyboard layout |
| `settings_no_updates` | Disable Windows updates |
| `settings_updates` | Run Windows updates |
| `settings_windows_defender` | Enable/disable Windows Defender |
| `settings_copy_files` | Copy files to target hosts |
| `settings_adjust_rights` | Adjust local group membership |
| `settings_user_rights` | Configure user rights assignments |
| `settings_disable_nat_adapter` | Disable NAT network adapter |
| `settings_enable_nat_adapter` | Enable NAT network adapter |
| `settings_gpmc` | Install Group Policy Management Console |
| `settings_gpo_remove` | Remove Group Policy Objects |

### Security

| Role | Description |
| ---- | ----------- |
| `security_account_is_sensitive` | Mark accounts as sensitive |
| `security_asr` | Configure Attack Surface Reduction |
| `security_audit_policy` | Configure audit policies |
| `security_enable_run_as_ppl` | Enable RunAsPPL for LSASS |
| `security_ensure_kb_not_installed` | Ensure specific KBs not installed |
| `security_powershell_restrict` | Restrict PowerShell execution |
| `dc_audit_sacl` | Configure DC SACL auditing |
| `ldap_diagnostic_logging` | Configure LDAP diagnostic logging |

### Vulnerabilities

| Role | Description |
| ---- | ----------- |
| `vulns_disable_firewall` | Disable Windows Firewall |
| `vulns_credentials` | Plant credentials in various locations |
| `vulns_autologon` | Configure autologon credentials |
| `vulns_shares` | Create vulnerable file shares |
| `vulns_openshares` | Create open file shares |
| `vulns_directory` | Create vulnerable directories |
| `vulns_files` | Deploy vulnerable files |
| `vulns_enable_llmnr` | Enable LLMNR |
| `vulns_enable_nbt_ns` | Enable NBT-NS |
| `vulns_smbv1` | Enable SMBv1 |
| `vulns_ntlmdowngrade` | Downgrade NTLM settings |
| `vulns_enable_credssp_client` | Enable CredSSP client |
| `vulns_enable_credssp_server` | Enable CredSSP server |
| `vulns_anonymous_enum` | Enable anonymous enumeration |
| `vulns_administrator_folder` | Create vulnerable admin folders |
| `vulns_permissions` | Configure vulnerable permissions |
| `vulns_acls` | Configure vulnerable ACLs |
| `vulns_schedule` | Create vulnerable scheduled tasks |
| `vulns_mssql` | Configure MSSQL vulnerabilities |
| `vulns_adcs_templates` | Deploy vulnerable ADCS templates |

### SCCM

| Role | Description |
| ---- | ----------- |
| `sccm_install_prerequisites` | Install SCCM prerequisites |
| `sccm_install_iis` | Install IIS for SCCM |
| `sccm_install_adk` | Install Windows ADK |
| `sccm_install_wsus` | Install WSUS |
| `sccm_install_mecm` | Install MECM/SCCM |
| `sccm_config_discovery` | Configure SCCM discovery |
| `sccm_config_boundary` | Configure SCCM boundaries |
| `sccm_config_accounts` | Configure SCCM accounts |
| `sccm_config_client_push` | Configure client push installation |
| `sccm_config_client_install` | Install SCCM client |
| `sccm_config_naa` | Configure network access account |
| `sccm_config_pxe` | Configure PXE boot |
| `sccm_config_users` | Configure SCCM users |
| `sccm_pxe` | Configure PXE deployment |

---

## Custom Modules

| Module | Description |
| ------ | ----------- |
| `win_ad_dacl` | Manage AD ACL/DACL entries |
| `win_ad_object` | Create/modify AD objects |
| `win_gpo` | Create/modify Group Policy Objects |
| `win_gpo_link` | Link GPOs to OUs |
| `win_gpo_reg` | Manage GPO registry settings |
| `sccm_boundary` | Manage SCCM boundaries |
| `sccm_boundary_group` | Manage SCCM boundary groups |
| `sccm_boundary_to_boundarygroup` | Map boundaries to groups |

---

## Usage

```yaml
---
- name: Deploy GOAD lab
  hosts: all
  collections:
    - dreadnode.goad

  roles:
    - role: dreadnode.goad.common
    - role: dreadnode.goad.domain_controller
```

For full orchestration, use the playbooks in the `ansible/playbooks/` directory with
the Taskfile:

```bash
task provision ENV=dev
```

---

## License

GPL-3.0-or-later

---

## Disclaimer

This collection deploys intentionally vulnerable configurations for security
research and penetration testing. **Do not use in production environments.**
