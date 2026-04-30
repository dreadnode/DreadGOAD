# Azure Bastion Terraform Module

<div align="center">

<img
  src="https://d1lppblt9t2x15.cloudfront.net/logos/5714928f3cdc09503751580cffbe8d02.png"
  alt="Logo"
  align="center"
  width="144px"
  height="144px"
/>

## Terraform module for optional Azure Bastion access in GOAD labs ☁️

_... managed with Terraform and GitHub Actions_ 🤖

</div>

---

## 📖 Overview

This Terraform module provisions an Azure Bastion host for DreadGOAD Azure
deployments without making Bastion part of the default lab footprint. It is
intended for operators who want browser-based access or native-client
`az network bastion ssh/rdp` connectivity to private lab VMs.

The module supports two deployment modes:

- `Developer`: shared Bastion infrastructure for low-cost dev/test access
- Dedicated `Basic`, `Standard`, or `Premium`: creates or reuses the required
  `AzureBastionSubnet` and Standard static public IP

The module creates:

- An optional `AzureBastionSubnet` named exactly as Azure requires
- An optional Standard static public IP for dedicated Bastion SKUs
- An Azure Bastion host with sane defaults for GOAD usage

Every resource gets a common tag set with `Module`, `Environment`, and
`ManagedBy = "Terraform"`, plus any caller-supplied `additional_tags`.

This module intentionally does **not** wire Bastion into the live Terragrunt
stack under `infra/azure/goad-deployment/`; Bastion remains opt-in because it
adds real hourly cost.

---

## Table of Contents

- [Usage](#usage)
- [Inputs](#inputs)
- [Outputs](#outputs)
- [Requirements](#requirements)
- [Development](#development)

---

## Usage

### Minimal dedicated Bastion

```hcl
module "bastion" {
  source = "../../modules/terraform-azure-bastion"

  env                  = "staging"
  deployment_name      = "goad"
  location             = module.network.location
  resource_group_name  = module.network.resource_group_name
  virtual_network_id   = module.network.vnet_id
  virtual_network_name = module.network.vnet_name
}
```

### Dedicated Bastion with native client support

```hcl
module "bastion" {
  source = "../../modules/terraform-azure-bastion"

  env                  = "staging"
  deployment_name      = "goad"
  location             = module.network.location
  resource_group_name  = module.network.resource_group_name
  virtual_network_id   = module.network.vnet_id
  virtual_network_name = module.network.vnet_name

  sku                    = "Standard"
  tunneling_enabled      = true
  file_copy_enabled      = true
  shareable_link_enabled = false

  additional_tags = {
    Project = "DreadGOAD"
  }
}
```

### Developer SKU

```hcl
module "bastion" {
  source = "../../modules/terraform-azure-bastion"

  env                 = "staging"
  deployment_name     = "goad"
  location            = module.network.location
  resource_group_name = module.network.resource_group_name
  virtual_network_id  = module.network.vnet_id

  sku = "Developer"
}
```

### Notes

- `sku = "Standard"` is the practical default for DreadGOAD. Set
  `tunneling_enabled = true` if you want native-client
  `az network bastion ssh/rdp` workflows.
- Dedicated Bastion SKUs require a subnet named `AzureBastionSubnet`; this
  module creates it by default unless you pass `bastion_subnet_id`.
- Dedicated Bastion SKUs require a Standard static public IP; this module
  creates it by default unless you pass `public_ip_id`.
- Developer SKU uses shared infrastructure, so this module does not create a
  subnet or public IP in that mode.
- Private-only Bastion is intentionally out of scope here. This repo currently
  standardizes on `azurerm`-only Azure modules, while private-only Bastion
  support is better handled via `azapi`.

---

<!-- markdownlint-disable -->
<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_azurerm"></a> [azurerm](#requirement\_azurerm) | ~> 4.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_azurerm"></a> [azurerm](#provider\_azurerm) | 4.70.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [azurerm_bastion_host.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/bastion_host) | resource |
| [azurerm_public_ip.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/public_ip) | resource |
| [azurerm_subnet.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Tags applied to every resource. | `map(string)` | `{}` | no |
| <a name="input_bastion_subnet_cidr"></a> [bastion\_subnet\_cidr](#input\_bastion\_subnet\_cidr) | CIDR for AzureBastionSubnet when the module creates it. Must be /26 or larger. | `string` | `"10.8.2.0/26"` | no |
| <a name="input_bastion_subnet_id"></a> [bastion\_subnet\_id](#input\_bastion\_subnet\_id) | Existing AzureBastionSubnet ID to use for dedicated Bastion SKUs. If null, the module creates one. | `string` | `null` | no |
| <a name="input_copy_paste_enabled"></a> [copy\_paste\_enabled](#input\_copy\_paste\_enabled) | Enable copy/paste support in Bastion sessions. | `bool` | `true` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Name of the deployment (e.g. "goad"). | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name (e.g. test, staging). | `string` | n/a | yes |
| <a name="input_file_copy_enabled"></a> [file\_copy\_enabled](#input\_file\_copy\_enabled) | Enable file copy support. Supported only on Standard and Premium. | `bool` | `false` | no |
| <a name="input_ip_connect_enabled"></a> [ip\_connect\_enabled](#input\_ip\_connect\_enabled) | Enable IP-based connections. Supported only on Standard and Premium. | `bool` | `false` | no |
| <a name="input_kerberos_enabled"></a> [kerberos\_enabled](#input\_kerberos\_enabled) | Enable Kerberos support. Supported only on Standard and Premium. | `bool` | `false` | no |
| <a name="input_location"></a> [location](#input\_location) | Azure region. | `string` | n/a | yes |
| <a name="input_public_ip_id"></a> [public\_ip\_id](#input\_public\_ip\_id) | Existing Standard static public IP to attach to dedicated Bastion SKUs. If null, the module creates one. | `string` | `null` | no |
| <a name="input_resource_group_name"></a> [resource\_group\_name](#input\_resource\_group\_name) | Resource group where the Bastion host and related resources are deployed. | `string` | n/a | yes |
| <a name="input_scale_units"></a> [scale\_units](#input\_scale\_units) | Scale units for the Bastion host. Standard and Premium support 2-50; Basic and Developer are fixed. | `number` | `2` | no |
| <a name="input_session_recording_enabled"></a> [session\_recording\_enabled](#input\_session\_recording\_enabled) | Enable session recording. Supported only on Premium. | `bool` | `false` | no |
| <a name="input_shareable_link_enabled"></a> [shareable\_link\_enabled](#input\_shareable\_link\_enabled) | Enable shareable links. Supported only on Standard and Premium. | `bool` | `false` | no |
| <a name="input_sku"></a> [sku](#input\_sku) | Azure Bastion SKU. Use Standard for native client/tunneling support; Developer is dev/test only and uses shared infrastructure. | `string` | `"Standard"` | no |
| <a name="input_tunneling_enabled"></a> [tunneling\_enabled](#input\_tunneling\_enabled) | Enable tunneling/native client support. Supported only on Standard and Premium. | `bool` | `false` | no |
| <a name="input_virtual_network_id"></a> [virtual\_network\_id](#input\_virtual\_network\_id) | VNet ID the Bastion host is attached to. Required for Developer SKU and used as an output anchor for dedicated SKUs. | `string` | `null` | no |
| <a name="input_virtual_network_name"></a> [virtual\_network\_name](#input\_virtual\_network\_name) | VNet name where the AzureBastionSubnet will be created for dedicated Bastion SKUs. | `string` | `null` | no |
| <a name="input_zones"></a> [zones](#input\_zones) | Availability zones for the Bastion host and created public IP. Null leaves the deployment unpinned. | `set(string)` | `null` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_bastion_host_id"></a> [bastion\_host\_id](#output\_bastion\_host\_id) | Azure Bastion host resource ID. |
| <a name="output_bastion_host_name"></a> [bastion\_host\_name](#output\_bastion\_host\_name) | Azure Bastion host resource name. |
| <a name="output_bastion_sku"></a> [bastion\_sku](#output\_bastion\_sku) | Azure Bastion SKU. |
| <a name="output_bastion_subnet_id"></a> [bastion\_subnet\_id](#output\_bastion\_subnet\_id) | AzureBastionSubnet ID for dedicated Bastion SKUs (null for Developer SKU). |
| <a name="output_public_ip_address"></a> [public\_ip\_address](#output\_public\_ip\_address) | Public IP address attached to Bastion (null for Developer SKU or when using an existing IP that is not created by this module). |
| <a name="output_public_ip_id"></a> [public\_ip\_id](#output\_public\_ip\_id) | Public IP resource ID attached to Bastion (null for Developer SKU). |
| <a name="output_resource_group_name"></a> [resource\_group\_name](#output\_resource\_group\_name) | Resource group containing the Bastion host. |
| <a name="output_virtual_network_id"></a> [virtual\_network\_id](#output\_virtual\_network\_id) | VNet ID the Bastion host is attached to. |
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
