<!-- DOCSIBLE START -->
# ludus_exchange

## Description

Install Exchange 2019 or Exchange 2016 to a Windows server

## Requirements

- Ansible >= 2.10

## Role Variables

### Default Variables (main.yml)

| Variable | Type | Default | Description |
| -------- | ---- | ------- | ----------- |
| `ludus_install_directory` | str | `/opt/ludus` | No description |
| `exchange_dotnet_install_path` | str | `https://download.visualstudio.microsoft.com/download/pr/2d6bb6b2-226a-4baa-bdec-798822606ff1/8494001c276a4b96804cde7829c04d7f/ndp48-x86-x64-allos-enu.exe` | No description |
| `vcredist2013_install_path` | str | `https://download.microsoft.com/download/2/E/6/2E61CFA4-993B-4DD4-91DA-3737CD5CD6E3/vcredist_x64.exe` | No description |
| `rewrite_module_path` | str | `https://download.microsoft.com/download/1/2/8/128E2E22-C1B9-44A4-BE2A-5859ED1D4592/rewrite_amd64_en-US.msi` | No description |
| `ucma_runtime_path` | str | `https://download.microsoft.com/download/2/C/4/2C47A5C1-A1F3-4843-B9FE-84C0032C61EC/UcmaRuntimeSetup.exe` | No description |
| `ludus_exchange_iso_url` | str | `https://download.microsoft.com/download/d/7/b/d7bcf78a-00d2-4a46-a3d2-7d506116bcd2/ExchangeServer2019-x64-CU9.ISO` | No description |
| `ludus_exchange2016_iso_url` | str | `https://download.microsoft.com/download/2/5/8/258D30CF-CA4C-433A-A618-FB7E6BCC4EEE/ExchangeServer2016-x64-cu12.iso` | No description |
| `ludus_exchange_domain` | str | `{{ (ludus | selectattr('vm_name', 'match', inventory_hostname))[0].domain.fqdn.split('.')[0] }}` | No description |
| `ludus_exchange_dc` | str | `{{ (ludus | selectattr('domain', 'defined') | selectattr('domain.fqdn', 'match', ludus_exchange_domain) | selectattr('domain.role', 'match', 'primary-dc'))[0].hostname }}` | No description |
| `ludus_exchange_host` | str | `{{ (ludus | selectattr('vm_name', 'match', inventory_hostname))[0].hostname }}` | No description |
| `ludus_exchange_domain_username` | str | `{{ ludus_exchange_domain }}\{{ defaults.ad_domain_admin }}` | No description |
| `ludus_exchange_domain_password` | str | `{{ defaults.ad_domain_admin_password }}` | No description |
| `send_connector_name` | str | `` | No description |
| `send_connector_smtpserver` | str | `` | No description |
| `send_connector_address_spaces` | list | `[]` | No description |
| `send_connector_address_spaces.0` | str | `SMTP:*;1` | No description |
| `send_connector_source_transport_servers` | str | `{{ (ludus | selectattr('vm_name', 'match', inventory_hostname))[0].hostname }}` | No description |

## Tasks

### ludus-create-mailbox.yml

- **Enable Mailbox for all AD users** (ansible.windows.win_shell)
- **Disable mailbox splash screen** (ansible.windows.win_shell)

### ludus-download-exchange-2016.yml

- **Get Exchange 2016 ISO if needed** (block)
- **Create resources ISO directory if it does not exist** (ansible.builtin.file)
- **Check if Exchange 2016 ISO exists** (ansible.builtin.stat)
- **Downloading EXCHANGE 2016 ISO - This will take a while** (ansible.builtin.get_url) - Conditional
- **Create EXCHANGE folder** (ansible.windows.win_file)
- **Check if EXCHANGE 2016 ISO exists on host** (ansible.windows.win_stat)
- **Copy Exchange ISO to windows host** (ansible.windows.win_copy) - Conditional

### ludus-download-exchange-2019.yml

- **Get Exchange ISO if needed** (block)
- **Create resources ISO directory if it does not exist** (ansible.builtin.file)
- **Check if Exchange ISO exists** (ansible.builtin.stat)
- **Downloading EXCHANGE ISO - This will take a while** (ansible.builtin.get_url) - Conditional
- **Create EXCHANGE folder** (ansible.windows.win_file)
- **Check if EXCHANGE ISO exists on host** (ansible.windows.win_stat)
- **Copy Exchange ISO to windows host** (ansible.windows.win_copy) - Conditional

### ludus-exchange-2016-install.yml

- **Mount Exchange 2016 ISO** (community.windows.win_disk_image)
- **Prepare Schema** (ansible.windows.win_shell)
- **Prepare Active Directory** (ansible.windows.win_shell)
- **Install Exchange 2016** (ansible.windows.win_shell)
- **Unmount ISO** (community.windows.win_disk_image)
- **Remove ISO file** (ansible.windows.win_file)

### ludus-exchange-2019-install.yml

- **Mount Exchange ISO** (community.windows.win_disk_image)
- **Prepare Schema** (ansible.windows.win_shell)
- **Prepare Active Directory** (ansible.windows.win_shell)
- **Install Exchange** (ansible.windows.win_shell)
- **Unmount ISO** (community.windows.win_disk_image)
- **Remove ISO file** (ansible.windows.win_file)

### ludus-exchange-dns.yml

- **Configure InternalDNSAdapter** (ansible.windows.win_powershell) - Conditional
- **Restart the Exchange Transport service** (ansible.windows.win_service) - Conditional

### ludus-exchange-pre.yml

- **Check if pre-req installation is completed** (ansible.windows.win_stat)
- **Install Exchange prerequisites** (block) - Conditional
- **Install IIS 6 Compatibility Features** (ansible.windows.win_feature)
- **Enable Windows optional features** (ansible.windows.win_optional_feature)
- **Install .NET Framework** (ansible.windows.win_package)
- **Reboot after installing .NET Framework 4.8** (ansible.windows.win_reboot) - Conditional
- **Wait for the machine to be up after reboot** (ansible.builtin.wait_for_connection)
- **Install Visual C++ Redistributable for Visual Studio 2013** (ansible.windows.win_package)
- **Reboot after installing Visual C++ Redistributable for Visual Studio 2013** (ansible.windows.win_reboot) - Conditional
- **The IIS URL Rewrite Module is required with Exchange Server 2016 CU22 and Exchange Server 2019 CU11 or later** (ansible.windows.win_package)
- **Install Unified Communications Managed API 4.0 Runtime.** (ansible.windows.win_package)
- **Reboot after installing Unified Communications Managed API 4.0 Runtime** (ansible.windows.win_reboot) - Conditional
- **Wait for the machine to be up after reboot** (ansible.builtin.wait_for_connection)
- **Install Windows features ADLDS Exchange Transport Hub** (ansible.windows.win_feature)
- **Create file marker for pre-req installation completion** (ansible.windows.win_shell) - Conditional
- **Reboot the system** (ansible.windows.win_reboot)
- **Wait for the machine to be up after reboot** (ansible.builtin.wait_for_connection)
- **Check if pre-req installation is already completed** (ansible.builtin.debug) - Conditional

### ludus_sendconnector.yml

- **Alert if send_connector_smtpserver is not set or empty** (ansible.builtin.debug) - Conditional
- **Configure Send Connector** (ansible.windows.win_shell) - Conditional

### main.yml

- **Check if Exchange is installed** (ansible.windows.win_service)
- **Download Exchange ISO for Windows Server 2016** (ansible.builtin.include_tasks) - Conditional
- **Download Exchange ISO for Windows Server 2019** (ansible.builtin.include_tasks) - Conditional
- **Ludus Exchange Server features to be installed** (ansible.builtin.include_tasks) - Conditional
- **Install Exchange Server for Windows Server 2016** (ansible.builtin.include_tasks) - Conditional
- **Install Exchange Server for Windows Server 2019** (ansible.builtin.include_tasks) - Conditional
- **Create ad users mailbox** (ansible.builtin.include_tasks)
- **Setup internal dns adapter** (ansible.builtin.include_tasks)

## Example Playbook

```yaml
- hosts: servers
  roles:
    - ludus_exchange
```

## Author Information

- **Author**: aleemladha
- **Company**: aleemladha
- **License**: GPLv3

## Platforms

- Windows: 2012R2, 2019, 2022
<!-- DOCSIBLE END -->
