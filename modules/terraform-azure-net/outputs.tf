output "resource_group_name" {
  value       = azurerm_resource_group.this.name
  description = "The shared resource group lab VMs deploy into."
}

output "location" {
  value       = azurerm_resource_group.this.location
  description = "Azure region for the deployment."
}

output "vnet_id" {
  value       = azurerm_virtual_network.this.id
  description = "VNet ID."
}

output "vnet_name" {
  value       = azurerm_virtual_network.this.name
  description = "VNet name."
}

output "vnet_cidr" {
  value       = var.vnet_cidr
  description = "VNet CIDR."
}

output "private_subnet_id" {
  value       = azurerm_subnet.private.id
  description = "Private subnet ID."
}

output "private_subnet_cidr" {
  value       = var.private_subnet_cidr
  description = "Private subnet CIDR."
}

output "public_subnet_id" {
  value       = var.create_public_subnet ? azurerm_subnet.public[0].id : null
  description = "Public subnet ID (null if not created)."
}

output "private_nsg_id" {
  value       = azurerm_network_security_group.private.id
  description = "NSG ID for the private subnet."
}

output "nat_public_ip" {
  value       = azurerm_public_ip.nat.ip_address
  description = "Public IP of the NAT gateway (egress IP)."
}
