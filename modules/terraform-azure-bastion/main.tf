locals {
  name_prefix = "${var.env}-${var.deployment_name}"

  is_developer           = var.sku == "Developer"
  is_standard_or_premium = contains(["Standard", "Premium"], var.sku)
  is_premium             = var.sku == "Premium"

  needs_bastion_subnet  = !local.is_developer && var.bastion_subnet_id == null
  create_bastion_subnet = local.needs_bastion_subnet && var.virtual_network_name != null
  create_public_ip      = !local.is_developer && var.public_ip_id == null

  bastion_subnet_id = local.is_developer ? null : (
    var.bastion_subnet_id != null ? var.bastion_subnet_id : (
      local.create_bastion_subnet ? azurerm_subnet.this[0].id : null
    )
  )

  public_ip_id = local.is_developer ? null : (
    var.public_ip_id != null ? var.public_ip_id : azurerm_public_ip.this[0].id
  )

  bastion_zones = var.zones == null ? null : sort(tolist(var.zones))

  base_tags = {
    Module      = "terraform-azure-bastion"
    Environment = var.env
    ManagedBy   = "Terraform"
  }

  tags = merge(local.base_tags, var.additional_tags)
}

resource "azurerm_subnet" "this" {
  count                = local.create_bastion_subnet ? 1 : 0
  name                 = "AzureBastionSubnet"
  resource_group_name  = var.resource_group_name
  virtual_network_name = var.virtual_network_name
  address_prefixes     = [var.bastion_subnet_cidr]
}

resource "azurerm_public_ip" "this" {
  count               = local.create_public_ip ? 1 : 0
  name                = "${local.name_prefix}-bastion-pip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  zones               = local.bastion_zones
  tags                = merge(local.tags, { Name = "${local.name_prefix}-bastion-pip" })
}

resource "azurerm_bastion_host" "this" {
  name                = "${local.name_prefix}-bastion"
  location            = var.location
  resource_group_name = var.resource_group_name

  sku = var.sku

  copy_paste_enabled        = var.copy_paste_enabled
  file_copy_enabled         = local.is_standard_or_premium ? var.file_copy_enabled : null
  ip_connect_enabled        = local.is_standard_or_premium ? var.ip_connect_enabled : null
  kerberos_enabled          = local.is_standard_or_premium ? var.kerberos_enabled : null
  scale_units               = local.is_standard_or_premium ? var.scale_units : null
  session_recording_enabled = local.is_premium ? var.session_recording_enabled : null
  shareable_link_enabled    = local.is_standard_or_premium ? var.shareable_link_enabled : null
  tunneling_enabled         = local.is_standard_or_premium ? var.tunneling_enabled : null
  virtual_network_id        = local.is_developer ? var.virtual_network_id : null
  zones                     = local.is_developer ? null : local.bastion_zones

  dynamic "ip_configuration" {
    for_each = local.is_developer ? [] : [1]

    content {
      name                 = "configuration"
      subnet_id            = local.bastion_subnet_id
      public_ip_address_id = local.public_ip_id
    }
  }

  tags = merge(local.tags, { Name = "${local.name_prefix}-bastion" })

  lifecycle {
    precondition {
      condition     = !local.is_developer || var.virtual_network_id != null
      error_message = "virtual_network_id is required when sku = \"Developer\"."
    }

    precondition {
      condition     = local.is_developer || !local.needs_bastion_subnet || var.virtual_network_name != null
      error_message = "virtual_network_name is required for dedicated Bastion SKUs so the module can create AzureBastionSubnet when needed."
    }

    precondition {
      condition     = local.is_developer || local.bastion_subnet_id != null
      error_message = "Dedicated Bastion SKUs require AzureBastionSubnet. Provide bastion_subnet_id or let the module create it."
    }

    precondition {
      condition     = local.is_developer || local.public_ip_id != null
      error_message = "Dedicated Bastion SKUs require a Standard static public IP. Provide public_ip_id or let the module create it."
    }

    precondition {
      condition = local.is_standard_or_premium || (
        !var.file_copy_enabled &&
        !var.ip_connect_enabled &&
        !var.kerberos_enabled &&
        !var.shareable_link_enabled &&
        !var.tunneling_enabled &&
        var.scale_units == 2
      )
      error_message = "file_copy_enabled, ip_connect_enabled, kerberos_enabled, shareable_link_enabled, tunneling_enabled, and custom scale_units require Standard or Premium SKU."
    }

    precondition {
      condition     = local.is_premium || !var.session_recording_enabled
      error_message = "session_recording_enabled requires Premium SKU."
    }
  }
}
