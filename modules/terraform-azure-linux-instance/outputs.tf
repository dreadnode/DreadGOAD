output "vm_id" {
  description = "Azure VM resource ID."
  value       = azurerm_linux_virtual_machine.this.id
}

output "vm_name" {
  description = "Azure VM resource name."
  value       = azurerm_linux_virtual_machine.this.name
}

output "computer_name" {
  description = "Linux hostname."
  value       = azurerm_linux_virtual_machine.this.computer_name
}

output "private_ip" {
  description = "Private IP address assigned to the NIC."
  value       = azurerm_network_interface.this.private_ip_address
}

output "principal_id" {
  description = "System-assigned managed identity principal ID."
  value       = azurerm_linux_virtual_machine.this.identity[0].principal_id
}

output "data_disk_ids" {
  description = "Managed data disk IDs keyed by logical disk name."
  value       = { for name, disk in azurerm_managed_disk.data : name => disk.id }
}
