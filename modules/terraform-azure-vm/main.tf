locals {
  name_prefix = var.vm_name

  common_tags = merge(
    var.tags,
    { ManagedBy = "Terraform" }
  )
}

resource "azurerm_resource_group" "this" {
  name     = "${local.name_prefix}-rg"
  location = var.location

  tags = merge(
    local.common_tags,
    { Name = "${local.name_prefix}-rg" }
  )
}

resource "random_password" "admin" {
  length           = 24
  special          = true
  override_special = "!@#$%^&*()-_=+"
  min_upper        = 2
  min_lower        = 2
  min_numeric      = 2
  min_special      = 2

  keepers = {
    vm_name = local.name_prefix
  }
}

resource "azurerm_windows_virtual_machine" "this" {
  name                  = "${local.name_prefix}-vm"
  resource_group_name   = azurerm_resource_group.this.name
  location              = azurerm_resource_group.this.location
  size                  = var.vm_size
  admin_username        = var.admin_username
  admin_password        = random_password.admin.result
  network_interface_ids = [azurerm_network_interface.this.id]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = var.os_disk_storage_account_type
  }

  source_image_reference {
    publisher = var.source_image.publisher
    offer     = var.source_image.offer
    sku       = var.source_image.sku
    version   = var.source_image.version
  }

  tags = merge(
    local.common_tags,
    { Name = "${local.name_prefix}-vm" }
  )
}
