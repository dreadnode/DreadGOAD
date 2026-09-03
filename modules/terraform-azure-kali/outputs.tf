output "vm_id" {
  description = "Azure VM resource ID for the Kali attack box."
  value       = azurerm_linux_virtual_machine.this.id
}

output "vm_name" {
  description = "Azure VM resource name for the Kali attack box."
  value       = azurerm_linux_virtual_machine.this.name
}

output "computer_name" {
  description = "Linux hostname assigned to the Kali attack box."
  value       = azurerm_linux_virtual_machine.this.computer_name
}

output "private_ip" {
  description = "Private IP address of the Kali VM's NIC."
  value       = azurerm_network_interface.this.private_ip_address
}

output "subnet_id" {
  description = "Subnet ID used by the Kali attack box."
  value       = local.create_dedicated_subnet ? azurerm_subnet.this[0].id : var.subnet_id
}

output "nsg_id" {
  description = "Dedicated Kali NSG ID, or null when an existing subnet is used."
  value       = local.create_dedicated_subnet ? azurerm_network_security_group.this[0].id : null
}

output "admin_username" {
  description = "Local admin username for the Kali VM."
  value       = var.admin_username
}

output "ssh_private_key_path" {
  description = "Filesystem path to the generated private key when the module created an ephemeral keypair; null when an explicit admin_ssh_public_key was supplied."
  value       = local.generate_ssh_key ? var.ephemeral_key_output_path : null
}

output "ssh_public_key_openssh" {
  description = "OpenSSH-formatted public key authorised on the Kali VM."
  value       = local.effective_public_key
}
