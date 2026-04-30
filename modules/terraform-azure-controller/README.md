# terraform-azure-controller

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_azurerm"></a> [azurerm](#requirement\_azurerm) | ~> 4.0 |
| <a name="requirement_local"></a> [local](#requirement\_local) | ~> 2.5 |
| <a name="requirement_tls"></a> [tls](#requirement\_tls) | ~> 4.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_azurerm"></a> [azurerm](#provider\_azurerm) | 4.70.0 |
| <a name="provider_local"></a> [local](#provider\_local) | 2.8.0 |
| <a name="provider_tls"></a> [tls](#provider\_tls) | 4.2.1 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [azurerm_linux_virtual_machine.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/linux_virtual_machine) | resource |
| [azurerm_network_interface.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_interface) | resource |
| [azurerm_network_security_group.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_security_group) | resource |
| [azurerm_subnet.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet) | resource |
| [azurerm_subnet_network_security_group_association.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/subnet_network_security_group_association) | resource |
| [local_sensitive_file.controller_key](https://registry.terraform.io/providers/hashicorp/local/latest/docs/resources/sensitive_file) | resource |
| [tls_private_key.controller](https://registry.terraform.io/providers/hashicorp/tls/latest/docs/resources/private_key) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Tags applied to every resource. | `map(string)` | `{}` | no |
| <a name="input_admin_ssh_public_key"></a> [admin\_ssh\_public\_key](#input\_admin\_ssh\_public\_key) | SSH public key authorised on the controller. When null, the module generates an ephemeral ed25519 keypair and writes the private key to ephemeral\_key\_output\_path. | `string` | `null` | no |
| <a name="input_admin_username"></a> [admin\_username](#input\_admin\_username) | Local admin username for the controller VM. | `string` | `"dreadadmin"` | no |
| <a name="input_ansible_galaxy_collections"></a> [ansible\_galaxy\_collections](#input\_ansible\_galaxy\_collections) | Galaxy collections installed at first boot. Override only if you need extra collections beyond the GOAD baseline. | `list(string)` | <pre>[<br/>  "ansible.windows",<br/>  "community.windows",<br/>  "microsoft.ad"<br/>]</pre> | no |
| <a name="input_controller_subnet_cidr"></a> [controller\_subnet\_cidr](#input\_controller\_subnet\_cidr) | CIDR for the controller's dedicated subnet. /28 is plenty for one VM. | `string` | `"10.8.3.0/28"` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Name of the deployment (e.g. "goad"). | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name (e.g. test, staging). | `string` | n/a | yes |
| <a name="input_ephemeral_key_output_path"></a> [ephemeral\_key\_output\_path](#input\_ephemeral\_key\_output\_path) | Filesystem path to write the generated private key when admin\_ssh\_public\_key is null. Required in that case; ignored when an explicit public key is supplied. | `string` | `null` | no |
| <a name="input_instance_size"></a> [instance\_size](#input\_instance\_size) | Azure VM size. B2s is the cheapest option that runs Ansible against a full GOAD lab without thrashing. | `string` | `"Standard_B2s"` | no |
| <a name="input_location"></a> [location](#input\_location) | Azure region. | `string` | n/a | yes |
| <a name="input_os_disk_size_gb"></a> [os\_disk\_size\_gb](#input\_os\_disk\_size\_gb) | Size of the OS disk in GB. 32 is enough for Ansible + checkout; bump if you cache lab artefacts on disk. | `number` | `32` | no |
| <a name="input_os_disk_storage_account_type"></a> [os\_disk\_storage\_account\_type](#input\_os\_disk\_storage\_account\_type) | Storage account type for the OS disk. | `string` | `"StandardSSD_LRS"` | no |
| <a name="input_resource_group_name"></a> [resource\_group\_name](#input\_resource\_group\_name) | Resource group the controller VM and its NIC/NSG/subnet are deployed into. | `string` | n/a | yes |
| <a name="input_source_image"></a> [source\_image](#input\_source\_image) | Marketplace image reference. Defaults to Ubuntu 24.04 LTS (Noble). | <pre>object({<br/>    publisher = string<br/>    offer     = string<br/>    sku       = string<br/>    version   = string<br/>  })</pre> | <pre>{<br/>  "offer": "ubuntu-24_04-lts",<br/>  "publisher": "Canonical",<br/>  "sku": "server",<br/>  "version": "latest"<br/>}</pre> | no |
| <a name="input_ssh_source_address_prefix"></a> [ssh\_source\_address\_prefix](#input\_ssh\_source\_address\_prefix) | Source allowed to reach the controller on TCP 22. Defaults to the AzureBastionSubnet CIDR — the only intended ingress path. | `string` | `"10.8.2.0/26"` | no |
| <a name="input_virtual_network_name"></a> [virtual\_network\_name](#input\_virtual\_network\_name) | VNet name where the controller subnet will be created. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_admin_username"></a> [admin\_username](#output\_admin\_username) | Local admin username for the controller VM. |
| <a name="output_computer_name"></a> [computer\_name](#output\_computer\_name) | Linux hostname assigned to the controller. |
| <a name="output_nsg_id"></a> [nsg\_id](#output\_nsg\_id) | NSG ID gating the controller subnet. |
| <a name="output_principal_id"></a> [principal\_id](#output\_principal\_id) | Managed identity principal ID for the controller VM. |
| <a name="output_private_ip"></a> [private\_ip](#output\_private\_ip) | Private IP address of the controller's NIC. |
| <a name="output_ssh_private_key_path"></a> [ssh\_private\_key\_path](#output\_ssh\_private\_key\_path) | Filesystem path to the generated private key when the module created an ephemeral keypair; null when an explicit admin\_ssh\_public\_key was supplied. |
| <a name="output_ssh_public_key_openssh"></a> [ssh\_public\_key\_openssh](#output\_ssh\_public\_key\_openssh) | OpenSSH-formatted public key authorised on the controller. |
| <a name="output_subnet_id"></a> [subnet\_id](#output\_subnet\_id) | Subnet ID created for the controller. |
| <a name="output_vm_id"></a> [vm\_id](#output\_vm\_id) | Azure VM resource ID for the controller. |
| <a name="output_vm_name"></a> [vm\_name](#output\_vm\_name) | Azure VM resource name for the controller. |
<!-- END_TF_DOCS -->
