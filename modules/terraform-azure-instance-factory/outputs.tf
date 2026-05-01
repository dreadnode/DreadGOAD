output "vm_id" {
  description = "Azure VM resource ID."
  value       = azurerm_windows_virtual_machine.this.id
}

output "vm_name" {
  description = "Azure VM resource name."
  value       = azurerm_windows_virtual_machine.this.name
}

output "computer_name" {
  description = "Windows NetBIOS computer name."
  value       = azurerm_windows_virtual_machine.this.computer_name
}

output "private_ip" {
  description = "Private IP assigned to the VM's NIC."
  value       = azurerm_network_interface.this.private_ip_address
}

output "public_ip" {
  description = "Public IP assigned to the VM (null if assign_public_ip = false)."
  value       = var.assign_public_ip ? azurerm_public_ip.this[0].ip_address : null
}

output "principal_id" {
  description = "Managed identity principal ID for the VM."
  value       = azurerm_windows_virtual_machine.this.identity[0].principal_id
}
