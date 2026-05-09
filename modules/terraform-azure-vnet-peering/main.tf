data "azurerm_virtual_network" "local" {
  name                = var.local_virtual_network_name
  resource_group_name = var.local_resource_group_name
}

data "azurerm_virtual_network" "remote" {
  name                = var.remote_virtual_network_name
  resource_group_name = var.remote_resource_group_name
}

resource "azurerm_virtual_network_peering" "to_remote" {
  name                         = "${var.name}-to-remote"
  resource_group_name          = var.local_resource_group_name
  virtual_network_name         = var.local_virtual_network_name
  remote_virtual_network_id    = data.azurerm_virtual_network.remote.id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = var.allow_forwarded_traffic
  allow_gateway_transit        = var.allow_gateway_transit
  use_remote_gateways          = var.use_remote_gateways
}

resource "azurerm_virtual_network_peering" "from_remote" {
  name                         = "${var.name}-from-remote"
  resource_group_name          = var.remote_resource_group_name
  virtual_network_name         = var.remote_virtual_network_name
  remote_virtual_network_id    = data.azurerm_virtual_network.local.id
  allow_virtual_network_access = true
  allow_forwarded_traffic      = var.allow_forwarded_traffic
  allow_gateway_transit        = false
  use_remote_gateways          = false
}

locals {
  remote_nsg_rg                = coalesce(var.remote_nsg_resource_group_name, var.remote_resource_group_name)
  remote_inbound_rules_enabled = var.remote_nsg_name != null && length(var.remote_inbound_allow_cidrs) > 0
  remote_inbound_rules         = local.remote_inbound_rules_enabled ? var.remote_inbound_allow_cidrs : []
}

resource "azurerm_network_security_rule" "remote_inbound_allow" {
  count = length(local.remote_inbound_rules)

  name                        = "${var.name}-allow-${count.index}"
  resource_group_name         = local.remote_nsg_rg
  network_security_group_name = var.remote_nsg_name
  priority                    = var.remote_inbound_priority_base + count.index
  direction                   = "Inbound"
  access                      = "Allow"
  protocol                    = "*"
  source_port_range           = "*"
  destination_port_range      = "*"
  source_address_prefix       = local.remote_inbound_rules[count.index]
  destination_address_prefix  = "*"
}
