output "admin_password" {
  description = "Generated admin password for the Windows VM."
  value       = random_password.admin.result
  sensitive   = true
}

output "admin_username" {
  description = "Local admin username on the Windows VM."
  value       = var.admin_username
}

output "network_security_group_id" {
  description = "ID of the network security group attached to the subnet."
  value       = azurerm_network_security_group.this.id
}

output "private_ip" {
  description = "Private IP address assigned to the VM's network interface."
  value       = azurerm_network_interface.this.private_ip_address
}

output "resource_group_name" {
  description = "Name of the resource group containing the VM."
  value       = azurerm_resource_group.this.name
}

output "subnet_id" {
  description = "ID of the subnet the VM is attached to."
  value       = azurerm_subnet.this.id
}

output "virtual_network_id" {
  description = "ID of the virtual network."
  value       = azurerm_virtual_network.this.id
}

output "vm_id" {
  description = "ID of the Windows virtual machine."
  value       = azurerm_windows_virtual_machine.this.id
}

output "vm_name" {
  description = "Name of the Windows virtual machine."
  value       = azurerm_windows_virtual_machine.this.name
}
