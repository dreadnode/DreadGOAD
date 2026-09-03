# terraform-azure-linux-instance

Creates one private Azure Linux virtual machine in an existing subnet. The
module supports deterministic private addresses, SSH-key authentication,
cloud-init, a system-assigned identity, and optional managed data disks.

The module deliberately does not create a subnet, NSG, public IP, or SSH key.
Those are deployment-level concerns, which allows multiple range hosts to
share one workload subnet and one operator key without conflicting resources.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_azurerm"></a> [azurerm](#requirement\_azurerm) | ~> 5.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_azurerm"></a> [azurerm](#provider\_azurerm) | 5.2.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [azurerm_linux_virtual_machine.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/linux_virtual_machine) | resource |
| [azurerm_managed_disk.data](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/managed_disk) | resource |
| [azurerm_network_interface.this](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/network_interface) | resource |
| [azurerm_virtual_machine_data_disk_attachment.data](https://registry.terraform.io/providers/hashicorp/azurerm/latest/docs/resources/virtual_machine_data_disk_attachment) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_admin_ssh_public_key"></a> [admin\_ssh\_public\_key](#input\_admin\_ssh\_public\_key) | OpenSSH public key authorized for the administrative user. | `string` | n/a | yes |
| <a name="input_admin_username"></a> [admin\_username](#input\_admin\_username) | Linux administrative user. | `string` | `"scopeadmin"` | no |
| <a name="input_computer_name"></a> [computer\_name](#input\_computer\_name) | Linux hostname. Defaults to instance\_name. | `string` | `""` | no |
| <a name="input_custom_data"></a> [custom\_data](#input\_custom\_data) | Cloud-init content. Azure receives it base64 encoded. | `string` | `""` | no |
| <a name="input_data_disks"></a> [data\_disks](#input\_data\_disks) | Additional managed data disks keyed by a stable logical name. | <pre>map(object({<br/>    lun                  = number<br/>    size_gb              = number<br/>    storage_account_type = optional(string, "StandardSSD_LRS")<br/>    caching              = optional(string, "ReadWrite")<br/>  }))</pre> | `{}` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Deployment name used in Azure resource names. | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name. | `string` | n/a | yes |
| <a name="input_instance_name"></a> [instance\_name](#input\_instance\_name) | Logical host name. | `string` | n/a | yes |
| <a name="input_instance_size"></a> [instance\_size](#input\_instance\_size) | Azure VM size. | `string` | `"Standard_D2s_v3"` | no |
| <a name="input_location"></a> [location](#input\_location) | Azure region. | `string` | n/a | yes |
| <a name="input_os_disk_size_gb"></a> [os\_disk\_size\_gb](#input\_os\_disk\_size\_gb) | OS disk size in GiB. | `number` | `64` | no |
| <a name="input_os_disk_storage_account_type"></a> [os\_disk\_storage\_account\_type](#input\_os\_disk\_storage\_account\_type) | Azure storage type for the OS disk. | `string` | `"StandardSSD_LRS"` | no |
| <a name="input_plan"></a> [plan](#input\_plan) | Optional Marketplace plan associated with the selected image. | <pre>object({<br/>    name      = string<br/>    product   = string<br/>    publisher = string<br/>  })</pre> | `null` | no |
| <a name="input_private_ip_address"></a> [private\_ip\_address](#input\_private\_ip\_address) | Optional static private IPv4 address. Dynamic allocation is used when null. | `string` | `null` | no |
| <a name="input_resource_group_name"></a> [resource\_group\_name](#input\_resource\_group\_name) | Resource group for the VM and its disks and NIC. | `string` | n/a | yes |
| <a name="input_source_image"></a> [source\_image](#input\_source\_image) | Marketplace image reference; ignored when source\_image\_id is set. | <pre>object({<br/>    publisher = string<br/>    offer     = string<br/>    sku       = string<br/>    version   = string<br/>  })</pre> | <pre>{<br/>  "offer": "ubuntu-24_04-lts",<br/>  "publisher": "Canonical",<br/>  "sku": "server",<br/>  "version": "latest"<br/>}</pre> | no |
| <a name="input_source_image_id"></a> [source\_image\_id](#input\_source\_image\_id) | Optional managed or Compute Gallery image resource ID. | `string` | `null` | no |
| <a name="input_subnet_id"></a> [subnet\_id](#input\_subnet\_id) | Existing subnet to attach the VM NIC to. | `string` | n/a | yes |
| <a name="input_tags"></a> [tags](#input\_tags) | Additional tags applied to all resources. | `map(string)` | `{}` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_computer_name"></a> [computer\_name](#output\_computer\_name) | Linux hostname. |
| <a name="output_data_disk_ids"></a> [data\_disk\_ids](#output\_data\_disk\_ids) | Managed data disk IDs keyed by logical disk name. |
| <a name="output_principal_id"></a> [principal\_id](#output\_principal\_id) | System-assigned managed identity principal ID. |
| <a name="output_private_ip"></a> [private\_ip](#output\_private\_ip) | Private IP address assigned to the NIC. |
| <a name="output_vm_id"></a> [vm\_id](#output\_vm\_id) | Azure VM resource ID. |
| <a name="output_vm_name"></a> [vm\_name](#output\_vm\_name) | Azure VM resource name. |
<!-- END_TF_DOCS -->