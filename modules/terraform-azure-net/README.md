# Azure Network Terraform Module

<div align="center">

<img
  src="https://d1lppblt9t2x15.cloudfront.net/logos/5714928f3cdc09503751580cffbe8d02.png"
  alt="Logo"
  align="center"
  width="144px"
  height="144px"
/>

## Terraform module for an Azure VNet with NAT-backed egress for GOAD labs ☁️

_... managed with Terraform and GitHub Actions_ 🤖

</div>

---

## 📖 Overview

This Terraform module creates the shared networking foundation that GOAD lab VMs
deploy into on Azure. It is the Azure counterpart to `terraform-aws-net` and is
intentionally narrow: a single VNet with a private subnet for lab VMs, an
optional public subnet for a jumpbox, a NAT gateway for outbound internet, and
an NSG that allows broad intra-VNet traffic so DCs and member servers can talk
to each other freely.

The module creates:

- A resource group named `${env}-${deployment_name}-rg`
- A virtual network with a single configurable address space
- A private subnet for lab VMs (DCs, member servers)
- An optional public subnet for a jumpbox/bastion
- A NAT gateway with a static public IP, associated to the private subnet, so
  marketplace Windows images can pull patches and installers during first-boot
  bootstrap
- An NSG attached to the private subnet that allows all intra-VNet traffic and
  Azure Load Balancer probes, and denies everything else inbound

Every resource gets a common tag set with `Module`, `Environment`, and
`ManagedBy = "Terraform"`, plus any caller-supplied `additional_tags`.

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
module "network" {
  source = "../../modules/terraform-azure-net"

  env             = "staging"
  deployment_name = "goad"
  location        = "centralus"
}
```

### With a public subnet and custom CIDRs

```hcl
module "network" {
  source = "../../modules/terraform-azure-net"

  env             = "staging"
  deployment_name = "goad"
  location        = "centralus"

  vnet_cidr            = "10.20.0.0/16"
  private_subnet_cidr  = "10.20.1.0/24"
  public_subnet_cidr   = "10.20.0.0/24"
  create_public_subnet = true

  additional_tags = {
    Project = "DreadGOAD"
  }
}
```

### Notes

- The NAT gateway is always created. It is the egress path for the private
  subnet so Windows VMs can reach the public internet without a public IP.
  The egress IP is exposed as the `nat_public_ip` output — handy for allowlisting.
- The default NSG allows all intra-VNet traffic. This is intentional for AD
  labs; do not reuse this module for production-style segmentation.
- The public subnet has no NSG of its own — attach one from the consumer if
  you put a jumpbox there.

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
| [azurerm_nat_gateway.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/nat_gateway) | resource |
| [azurerm_nat_gateway_public_ip_association.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/nat_gateway_public_ip_association) | resource |
| [azurerm_network_security_group.private](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_security_group) | resource |
| [azurerm_public_ip.nat](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/public_ip) | resource |
| [azurerm_resource_group.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/resource_group) | resource |
| [azurerm_subnet.private](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet) | resource |
| [azurerm_subnet.public](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet) | resource |
| [azurerm_subnet_nat_gateway_association.private](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_nat_gateway_association) | resource |
| [azurerm_subnet_network_security_group_association.private](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_network_security_group_association) | resource |
| [azurerm_virtual_network.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/virtual_network) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Tags applied to every resource. | `map(string)` | `{}` | no |
| <a name="input_create_public_subnet"></a> [create\_public\_subnet](#input\_create\_public\_subnet) | Whether to create a public subnet. | `bool` | `false` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Name of the deployment (e.g. "goad"). | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name (e.g. test, staging). | `string` | n/a | yes |
| <a name="input_location"></a> [location](#input\_location) | Azure region. | `string` | n/a | yes |
| <a name="input_private_subnet_cidr"></a> [private\_subnet\_cidr](#input\_private\_subnet\_cidr) | CIDR for the private subnet where lab VMs run. | `string` | `"10.8.1.0/24"` | no |
| <a name="input_public_subnet_cidr"></a> [public\_subnet\_cidr](#input\_public\_subnet\_cidr) | CIDR for the public subnet (jumpbox/bastion). | `string` | `"10.8.0.0/24"` | no |
| <a name="input_vnet_cidr"></a> [vnet\_cidr](#input\_vnet\_cidr) | CIDR block for the VNet. | `string` | `"10.8.0.0/16"` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_location"></a> [location](#output\_location) | Azure region for the deployment. |
| <a name="output_nat_public_ip"></a> [nat\_public\_ip](#output\_nat\_public\_ip) | Public IP of the NAT gateway (egress IP). |
| <a name="output_private_nsg_id"></a> [private\_nsg\_id](#output\_private\_nsg\_id) | NSG ID for the private subnet. |
| <a name="output_private_subnet_cidr"></a> [private\_subnet\_cidr](#output\_private\_subnet\_cidr) | Private subnet CIDR. |
| <a name="output_private_subnet_id"></a> [private\_subnet\_id](#output\_private\_subnet\_id) | Private subnet ID. |
| <a name="output_public_subnet_id"></a> [public\_subnet\_id](#output\_public\_subnet\_id) | Public subnet ID (null if not created). |
| <a name="output_resource_group_name"></a> [resource\_group\_name](#output\_resource\_group\_name) | The shared resource group lab VMs deploy into. |
| <a name="output_vnet_cidr"></a> [vnet\_cidr](#output\_vnet\_cidr) | VNet CIDR. |
| <a name="output_vnet_id"></a> [vnet\_id](#output\_vnet\_id) | VNet ID. |
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
