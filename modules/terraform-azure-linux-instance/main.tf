locals {
  name_prefix   = "${var.env}-${var.deployment_name}-${var.instance_name}"
  computer_name = var.computer_name != "" ? var.computer_name : var.instance_name

  common_tags = merge(
    var.tags,
    {
      Environment  = var.env
      ManagedBy    = "Terraform"
      AccessMethod = "BastionSSH"
      Module       = "terraform-azure-linux-instance"
    },
  )
}

resource "azurerm_network_interface" "this" {
  name                = "${local.name_prefix}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet_id
    private_ip_address_allocation = var.private_ip_address == null ? "Dynamic" : "Static"
    private_ip_address            = var.private_ip_address
  }

  tags = merge(local.common_tags, { Name = "${local.name_prefix}-nic" })
}

resource "azurerm_linux_virtual_machine" "this" {
  name                = "${local.name_prefix}-vm"
  computer_name       = local.computer_name
  resource_group_name = var.resource_group_name
  location            = var.location
  size                = var.instance_size
  admin_username      = var.admin_username

  network_interface_ids = [azurerm_network_interface.this.id]

  disable_password_authentication = true

  admin_ssh_key {
    username   = var.admin_username
    public_key = trimspace(var.admin_ssh_public_key)
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = var.os_disk_storage_account_type
    disk_size_gb         = var.os_disk_size_gb
  }

  source_image_id = var.source_image_id

  dynamic "source_image_reference" {
    for_each = var.source_image_id == null ? [var.source_image] : []
    content {
      publisher = source_image_reference.value.publisher
      offer     = source_image_reference.value.offer
      sku       = source_image_reference.value.sku
      version   = source_image_reference.value.version
    }
  }

  dynamic "plan" {
    for_each = var.plan == null ? [] : [var.plan]
    content {
      name      = plan.value.name
      product   = plan.value.product
      publisher = plan.value.publisher
    }
  }

  custom_data = var.custom_data == "" ? null : base64encode(var.custom_data)

  identity {
    type = "SystemAssigned"
  }

  tags = merge(local.common_tags, { Name = "${local.name_prefix}-vm" })
}

resource "azurerm_managed_disk" "data" {
  for_each = var.data_disks

  name                 = "${local.name_prefix}-${each.key}"
  location             = var.location
  resource_group_name  = var.resource_group_name
  storage_account_type = each.value.storage_account_type
  create_option        = "Empty"
  disk_size_gb         = each.value.size_gb

  tags = merge(local.common_tags, { Name = "${local.name_prefix}-${each.key}" })
}

resource "azurerm_virtual_machine_data_disk_attachment" "data" {
  for_each = var.data_disks

  managed_disk_id    = azurerm_managed_disk.data[each.key].id
  virtual_machine_id = azurerm_linux_virtual_machine.this.id
  lun                = each.value.lun
  caching            = each.value.caching
}
