output "local_to_remote_peering_id" {
  value       = azurerm_virtual_network_peering.to_remote.id
  description = "Peering resource ID on the local VNet."
}

output "remote_to_local_peering_id" {
  value       = azurerm_virtual_network_peering.from_remote.id
  description = "Peering resource ID on the remote VNet."
}

output "local_vnet_id" {
  value       = data.azurerm_virtual_network.local.id
  description = "Resolved local VNet ID."
}

output "remote_vnet_id" {
  value       = data.azurerm_virtual_network.remote.id
  description = "Resolved remote VNet ID."
}

output "remote_vnet_address_space" {
  value       = data.azurerm_virtual_network.remote.address_space
  description = "Address space of the remote VNet — useful to feed into NSG rules on the local side."
}
