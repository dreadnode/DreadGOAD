# terraform-azure-vnet-peering

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_azurerm"></a> [azurerm](#requirement\_azurerm) | ~> 4.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_azurerm"></a> [azurerm](#provider\_azurerm) | 4.71.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [azurerm_network_security_rule.remote_inbound_allow](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_security_rule) | resource |
| [azurerm_virtual_network_peering.from_remote](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/virtual_network_peering) | resource |
| [azurerm_virtual_network_peering.to_remote](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/virtual_network_peering) | resource |
| [azurerm_virtual_network.local](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/data-sources/virtual_network) | data source |
| [azurerm_virtual_network.remote](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/data-sources/virtual_network) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_allow_forwarded_traffic"></a> [allow\_forwarded\_traffic](#input\_allow\_forwarded\_traffic) | Whether traffic forwarded from outside (e.g. via NVA) is allowed across the peering. GOAD attacker traffic is sourced directly from peered subnets, so default off. | `bool` | `false` | no |
| <a name="input_allow_gateway_transit"></a> [allow\_gateway\_transit](#input\_allow\_gateway\_transit) | Whether the local VNet allows the remote VNet to use its gateway. Lab peerings don't share gateways. | `bool` | `false` | no |
| <a name="input_local_resource_group_name"></a> [local\_resource\_group\_name](#input\_local\_resource\_group\_name) | Resource group of the local VNet (the side this stack 'owns' first-class). | `string` | n/a | yes |
| <a name="input_local_virtual_network_name"></a> [local\_virtual\_network\_name](#input\_local\_virtual\_network\_name) | Name of the local VNet. | `string` | n/a | yes |
| <a name="input_name"></a> [name](#input\_name) | Logical name used to derive the peering resource names. Both directions get suffixes (-to-remote and -from-remote). | `string` | n/a | yes |
| <a name="input_remote_inbound_allow_cidrs"></a> [remote\_inbound\_allow\_cidrs](#input\_remote\_inbound\_allow\_cidrs) | CIDRs on the local side that should be allowed inbound on the remote NSG. Ignored unless remote\_nsg\_name is set. Adds one allow rule per entry. | `list(string)` | `[]` | no |
| <a name="input_remote_inbound_priority_base"></a> [remote\_inbound\_priority\_base](#input\_remote\_inbound\_priority\_base) | Starting NSG rule priority for the generated allow rules. Each subsequent CIDR increments by 1. Pick a band that doesn't collide with existing rules. | `number` | `200` | no |
| <a name="input_remote_nsg_name"></a> [remote\_nsg\_name](#input\_remote\_nsg\_name) | Optional NSG on the remote side to open for ingress from local CIDRs. The remote NSG's default DenyAllInbound means peered traffic is dropped unless explicitly allowed; set this to the GOAD/lab private-subnet NSG when peering an attacker workstation in. Null = skip. | `string` | `null` | no |
| <a name="input_remote_nsg_resource_group_name"></a> [remote\_nsg\_resource\_group\_name](#input\_remote\_nsg\_resource\_group\_name) | Resource group of the remote NSG. Defaults to remote\_resource\_group\_name (typical case: NSG sits in the same RG as the remote VNet). | `string` | `null` | no |
| <a name="input_remote_resource_group_name"></a> [remote\_resource\_group\_name](#input\_remote\_resource\_group\_name) | Resource group of the remote VNet. | `string` | n/a | yes |
| <a name="input_remote_virtual_network_name"></a> [remote\_virtual\_network\_name](#input\_remote\_virtual\_network\_name) | Name of the remote VNet. The module looks it up via data source so the caller doesn't need to plumb the full ID. | `string` | n/a | yes |
| <a name="input_use_remote_gateways"></a> [use\_remote\_gateways](#input\_use\_remote\_gateways) | Whether the local VNet uses the remote VNet's gateway. Mutually exclusive with allow\_gateway\_transit on the same side. | `bool` | `false` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_local_to_remote_peering_id"></a> [local\_to\_remote\_peering\_id](#output\_local\_to\_remote\_peering\_id) | Peering resource ID on the local VNet. |
| <a name="output_local_vnet_id"></a> [local\_vnet\_id](#output\_local\_vnet\_id) | Resolved local VNet ID. |
| <a name="output_remote_to_local_peering_id"></a> [remote\_to\_local\_peering\_id](#output\_remote\_to\_local\_peering\_id) | Peering resource ID on the remote VNet. |
| <a name="output_remote_vnet_address_space"></a> [remote\_vnet\_address\_space](#output\_remote\_vnet\_address\_space) | Address space of the remote VNet — useful to feed into NSG rules on the local side. |
| <a name="output_remote_vnet_id"></a> [remote\_vnet\_id](#output\_remote\_vnet\_id) | Resolved remote VNet ID. |
<!-- END_TF_DOCS -->
