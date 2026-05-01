output "bastion_host_id" {
  description = "Azure Bastion host resource ID."
  value       = azurerm_bastion_host.this.id
}

output "bastion_host_name" {
  description = "Azure Bastion host resource name."
  value       = azurerm_bastion_host.this.name
}

output "bastion_sku" {
  description = "Azure Bastion SKU."
  value       = var.sku
}

output "resource_group_name" {
  description = "Resource group containing the Bastion host."
  value       = var.resource_group_name
}

output "virtual_network_id" {
  description = "VNet ID the Bastion host is attached to."
  value       = var.virtual_network_id
}

output "bastion_subnet_id" {
  description = "AzureBastionSubnet ID for dedicated Bastion SKUs (null for Developer SKU)."
  value       = local.bastion_subnet_id
}

output "public_ip_id" {
  description = "Public IP resource ID attached to Bastion (null for Developer SKU)."
  value       = local.public_ip_id
}

output "public_ip_address" {
  description = "Public IP address attached to Bastion (null for Developer SKU or when using an existing IP that is not created by this module)."
  value       = local.create_public_ip ? azurerm_public_ip.this[0].ip_address : null
}
