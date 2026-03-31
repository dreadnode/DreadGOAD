# dreadnode.goad

Ansible collection for deploying and configuring vulnerable Active Directory
lab environments for penetration testing and security research.

Based on [GOAD (Game of Active Directory)](https://github.com/Orange-Cyberdefense/GOAD)
by Orange Cyberdefense.

---

## Architecture Diagram

```mermaid
graph TD
    Collection[Ansible Collection]
    Collection --> Roles[⚙️ Roles]
    Roles --> R0[vulns_credentials]
    Roles --> R1[sccm_install_wsus]
    Roles --> R2[sccm_pxe]
    Roles --> R3[sccm_install_iis]
    Roles --> R4[vulns_ntlmdowngrade]
    Roles --> R5[trusts]
    Roles --> R6[mssql_reporting]
    Roles --> R7[domain_controller_slave]
    Roles --> R8[disable_user]
    Roles --> R9[settings_copy_files]
    Roles --> R10[vulns_mssql]
    Roles --> R11[laps_verify]
    Roles --> R12[ad]
    Roles --> R13[vulns_enable_credssp_server]
    Roles --> R14[sccm_install_mecm]
    Roles --> R15[dc_audit_sacl]
    Roles --> R16[security_ensure_kb_not_installed]
    Roles --> R17[vulns_openshares]
    Roles --> R18[sync_domains]
    Roles --> R19[sccm_config_client_push]
    Roles --> R20[vulns_schedule]
    Roles --> R21[sccm_config_pxe]
    Roles --> R22[vulns_shares]
    Roles --> R23[laps_dc]
    Roles --> R24[settings_updates]
    Roles --> R25[groups_domains]
    Roles --> R26[vulns_anonymous_enum]
    Roles --> R27[sccm_config_client_install]
    Roles --> R28[mssql_audit]
    Roles --> R29[vulns_enable_llmnr]
    Roles --> R30[sccm_config_accounts]
    Roles --> R31[settings_admin_password]
    Roles --> R32[vulns_acls]
    Roles --> R33[security_enable_run_as_ppl]
    Roles --> R34[gmsa_hosts]
    Roles --> R35[onlyusers]
    Roles --> R36[child_domain]
    Roles --> R37[sccm_install_adk]
    Roles --> R38[mssql_link]
    Roles --> R39[vulns_files]
    Roles --> R40[parent_child_dns]
    Roles --> R41[adcs_templates]
    Roles --> R42[laps_server]
    Roles --> R43[settings_enable_nat_adapter]
    Roles --> R44[elk]
    Roles --> R45[sccm_install_prerequisites]
    Roles --> R46[vulns_permissions]
    Roles --> R47[sccm_config_discovery]
    Roles --> R48[settings_windows_defender]
    Roles --> R49[member_server]
    Roles --> R50[dc_dns_conditional_forwarder]
    Roles --> R51[common]
    Roles --> R52[sccm_config_boundary]
    Roles --> R53[ps]
    Roles --> R54[adcs]
    Roles --> R55[enable_user]
    Roles --> R56[laps_permissions]
    Roles --> R57[dns_conditional_forwarder]
    Roles --> R58[sccm_config_users]
    Roles --> R59[vulns_smbv1]
    Roles --> R60[ldap_diagnostic_logging]
    Roles --> R61[vulns_enable_credssp_client]
    Roles --> R62[dhcp]
    Roles --> R63[localusers]
    Roles --> R64[sccm_config_naa]
    Roles --> R65[password_policy]
    Roles --> R66[security_powershell_restrict]
    Roles --> R67[settings_keyboard]
    Roles --> R68[vulns_autologon]
    Roles --> R69[settings_user_rights]
    Roles --> R70[commonwkstn]
    Roles --> R71[vulns_enable_nbt_ns]
    Roles --> R72[mssql_ssms]
    Roles --> R73[webdav]
    Roles --> R74[settings_gpo_remove]
    Roles --> R75[settings_adjust_rights]
    Roles --> R76[vulns_disable_firewall]
    Roles --> R77[vulns_adcs_templates]
    Roles --> R78[gmsa]
    Roles --> R79[settings_gpmc]
    Roles --> R80[settings_disable_nat_adapter]
    Roles --> R81[security_account_is_sensitive]
    Roles --> R82[domain_controller]
    Roles --> R83[fix_dns]
    Roles --> R84[vulns_administrator_folder]
    Roles --> R85[iis]
    Roles --> R86[move_to_ou]
    Roles --> R87[vulns_directory]
    Roles --> R88[mssql]
    Roles --> R89[acl]
    Roles --> R90[settings_no_updates]
    Roles --> R91[logs_windows]
    Roles --> R92[security_audit_policy]
    Roles --> R93[security_asr]
    Roles --> R94[settings_hostname]
    Collection --> Playbooks[📚 Playbooks]
    Playbooks --> PB0[base]
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
