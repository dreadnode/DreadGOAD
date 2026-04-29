# Azure Windows VM Terraform Module

<div align="center">

<img
  src="https://d1lppblt9t2x15.cloudfront.net/logos/5714928f3cdc09503751580cffbe8d02.png"
  alt="Logo"
  align="center"
  width="144px"
  height="144px"
/>

## Terraform module for an Azure Windows VM with self-contained networking ☁️

_... managed with Terraform and GitHub Actions_ 🤖

</div>

---

## 📖 Overview

This Terraform module provisions a single Windows virtual machine in Azure
together with the surrounding networking primitives required to run it. It is
intended as a building block for DreadGOAD's Azure provider — for example, the
auth-validation environment used to confirm Azure credentials and provider
plumbing.

The module creates:

- A resource group that owns every resource the module manages
- A virtual network and a single subnet
- A network security group bound to the subnet
- A network interface attached to the subnet (dynamic private IP)
- A Windows VM (default: Server 2022 Datacenter, Azure Edition) with a
  randomly generated local admin password
- A `random_password` resource for the local admin account, exposed as a
  sensitive output

Everything is named off a single `vm_name` prefix, and every resource gets a
common tag set with `ManagedBy = "Terraform"` plus any caller-supplied tags.

---

## Table of Contents

- [Usage](#usage)
- [Inputs](#inputs)
- [Outputs](#outputs)
- [Requirements](#requirements)
- [Development](#development)

---

## Usage

### Minimal example

```hcl
module "vm" {
  source = "../../modules/terraform-azure-vm"

  vm_name  = "auth-validation"
  location = "eastus"
}
```

### With a custom VNet, size, and tags

```hcl
module "vm" {
  source = "../../modules/terraform-azure-vm"

  vm_name        = "auth-validation"
  location       = "eastus"
  vm_size        = "Standard_D2s_v5"
  address_space  = "10.50.0.0/16"
  subnet_cidr    = "10.50.1.0/24"
  admin_username = "goadadmin"

  source_image = {
    publisher = "MicrosoftWindowsServer"
    offer     = "WindowsServer"
    sku       = "2022-datacenter-azure-edition"
    version   = "latest"
  }

  tags = {
    Project = "DreadGOAD"
    Env     = "dev"
  }
}
```

### Retrieving the generated admin password

The local admin password is generated at apply time and exposed as a sensitive
output. Pull it with:

```bash
terraform output -raw admin_password
```

### Notes

- The module is intentionally narrow: it does not create a public IP, bastion,
  or any inbound NSG rules. The default NSG is empty, so the VM is reachable
  only from inside the VNet.
- `random_password` is keyed off `vm_name`, so the password is stable as long
  as `vm_name` does not change.
- The module manages its own resource group; do not point it at a
  pre-existing resource group.

---

<!-- markdownlint-disable -->
<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
|------|---------|
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | ~> 1.7 |
| <a name="requirement_azurerm"></a> [azurerm](#requirement\_azurerm) | ~> 4.0 |
| <a name="requirement_random"></a> [random](#requirement\_random) | ~> 3.6 |

## Providers

| Name | Version |
|------|---------|
| <a name="provider_azurerm"></a> [azurerm](#provider\_azurerm) | ~> 4.0 |
| <a name="provider_random"></a> [random](#provider\_random) | ~> 3.6 |

## Modules

No modules.

## Resources

| Name | Type |
|------|------|
| [azurerm_network_interface.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_interface) | resource |
| [azurerm_network_security_group.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_security_group) | resource |
| [azurerm_resource_group.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/resource_group) | resource |
| [azurerm_subnet.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet) | resource |
| [azurerm_subnet_network_security_group_association.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_network_security_group_association) | resource |
| [azurerm_virtual_network.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/virtual_network) | resource |
| [azurerm_windows_virtual_machine.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/windows_virtual_machine) | resource |
| [random_password.admin](https://registry.terraform.io/providers/hashicorp/random/latest/docs/resources/password) | resource |

## Inputs

| Name | Description | Type | Default | Required |
|------|-------------|------|---------|:--------:|
| <a name="input_address_space"></a> [address\_space](#input\_address\_space) | VNet CIDR block. | `string` | `"10.40.0.0/16"` | no |
| <a name="input_admin_username"></a> [admin\_username](#input\_admin\_username) | Local admin username on the Windows VM. | `string` | `"goadadmin"` | no |
| <a name="input_location"></a> [location](#input\_location) | Azure region (e.g. eastus). | `string` | n/a | yes |
| <a name="input_os_disk_storage_account_type"></a> [os\_disk\_storage\_account\_type](#input\_os\_disk\_storage\_account\_type) | Storage account type for the OS disk. | `string` | `"StandardSSD_LRS"` | no |
| <a name="input_source_image"></a> [source\_image](#input\_source\_image) | Marketplace image reference for the Windows VM. | <pre>object({<br/>    publisher = string<br/>    offer     = string<br/>    sku       = string<br/>    version   = string<br/>  })</pre> | <pre>{<br/>  "offer": "WindowsServer",<br/>  "publisher": "MicrosoftWindowsServer",<br/>  "sku": "2022-datacenter-azure-edition",<br/>  "version": "latest"<br/>}</pre> | no |
| <a name="input_subnet_cidr"></a> [subnet\_cidr](#input\_subnet\_cidr) | Subnet CIDR inside the VNet. | `string` | `"10.40.1.0/24"` | no |
| <a name="input_tags"></a> [tags](#input\_tags) | Additional tags applied to every resource. | `map(string)` | `{}` | no |
| <a name="input_vm_name"></a> [vm\_name](#input\_vm\_name) | Name of the VM (used to derive resource names). | `string` | n/a | yes |
| <a name="input_vm_size"></a> [vm\_size](#input\_vm\_size) | Azure VM size. | `string` | `"Standard_B2s"` | no |

## Outputs

| Name | Description |
|------|-------------|
| <a name="output_admin_password"></a> [admin\_password](#output\_admin\_password) | Generated admin password for the Windows VM. |
| <a name="output_admin_username"></a> [admin\_username](#output\_admin\_username) | Local admin username on the Windows VM. |
| <a name="output_network_security_group_id"></a> [network\_security\_group\_id](#output\_network\_security\_group\_id) | ID of the network security group attached to the subnet. |
| <a name="output_private_ip"></a> [private\_ip](#output\_private\_ip) | Private IP address assigned to the VM's network interface. |
| <a name="output_resource_group_name"></a> [resource\_group\_name](#output\_resource\_group\_name) | Name of the resource group containing the VM. |
| <a name="output_subnet_id"></a> [subnet\_id](#output\_subnet\_id) | ID of the subnet the VM is attached to. |
| <a name="output_virtual_network_id"></a> [virtual\_network\_id](#output\_virtual\_network\_id) | ID of the virtual network. |
| <a name="output_vm_id"></a> [vm\_id](#output\_vm\_id) | ID of the Windows virtual machine. |
| <a name="output_vm_name"></a> [vm\_name](#output\_vm\_name) | Name of the Windows virtual machine. |
<!-- END_TF_DOCS -->
<!-- markdownlint-restore -->

---

## Development

### Prerequisites

- [Terraform](https://www.terraform.io/downloads.html)
- [pre-commit](https://pre-commit.com/#install)
- [terraform-docs](https://github.com/terraform-docs/terraform-docs)
  (used by pre-commit hook)

### Pre-Commit Hooks

Install, update, and run pre-commit hooks:

```bash
export TASK_X_REMOTE_TASKFILES=1 && \
task run-pre-commit -y
```
