output "vm_id" {
  description = "Azure VM resource ID for the controller."
  value       = azurerm_linux_virtual_machine.this.id
}

output "vm_name" {
  description = "Azure VM resource name for the controller."
  value       = azurerm_linux_virtual_machine.this.name
}

output "computer_name" {
  description = "Linux hostname assigned to the controller."
  value       = azurerm_linux_virtual_machine.this.computer_name
}

output "private_ip" {
  description = "Private IP address of the controller's NIC."
  value       = azurerm_network_interface.this.private_ip_address
}

output "subnet_id" {
  description = "Subnet ID created for the controller."
  value       = azurerm_subnet.this.id
}

output "nsg_id" {
  description = "NSG ID gating the controller subnet."
  value       = azurerm_network_security_group.this.id
}

output "principal_id" {
  description = "Managed identity principal ID for the controller VM."
  value       = azurerm_linux_virtual_machine.this.identity[0].principal_id
}

output "admin_username" {
  description = "Local admin username for the controller VM."
  value       = var.admin_username
}

output "ssh_private_key_path" {
  description = "Filesystem path to the generated private key when the module created an ephemeral keypair; null when an explicit admin_ssh_public_key was supplied."
  value       = local.generate_ssh_key ? var.ephemeral_key_output_path : null
}

output "ssh_public_key_openssh" {
  description = "OpenSSH-formatted public key authorised on the controller."
  value       = local.effective_public_key
}
