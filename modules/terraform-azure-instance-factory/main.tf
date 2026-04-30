locals {
  name_prefix = "${var.env}-${var.instance_name}"

  computer_name = var.computer_name != "" ? var.computer_name : substr(replace(local.name_prefix, "_", "-"), 0, 15)

  common_tags = merge(
    var.tags,
    {
      Environment  = var.env
      ManagedBy    = "Terraform"
      AccessMethod = "AzureRunCommand"
    },
  )
}

resource "azurerm_public_ip" "this" {
  count               = var.assign_public_ip ? 1 : 0
  name                = "${local.name_prefix}-pip"
  location            = var.location
  resource_group_name = var.resource_group_name
  allocation_method   = "Static"
  sku                 = "Standard"
  tags                = merge(local.common_tags, { Name = "${local.name_prefix}-pip" })
}

resource "azurerm_network_interface" "this" {
  name                = "${local.name_prefix}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet_id
    private_ip_address_allocation = "Dynamic"
    public_ip_address_id          = var.assign_public_ip ? azurerm_public_ip.this[0].id : null
  }

  tags = merge(local.common_tags, { Name = "${local.name_prefix}-nic" })
}

resource "azurerm_windows_virtual_machine" "this" {
  name                  = "${local.name_prefix}-vm"
  computer_name         = local.computer_name
  resource_group_name   = var.resource_group_name
  location              = var.location
  size                  = var.instance_size
  admin_username        = var.admin_username
  admin_password        = var.admin_password
  network_interface_ids = [azurerm_network_interface.this.id]

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = var.os_disk_storage_account_type
    disk_size_gb         = var.os_disk_size_gb
  }

  source_image_reference {
    publisher = var.source_image.publisher
    offer     = var.source_image.offer
    sku       = var.source_image.sku
    version   = var.source_image.version
  }

  identity {
    type = "SystemAssigned"
  }

  tags = merge(
    local.common_tags,
    { Name = "${local.name_prefix}-vm" },
  )
}

# Custom Script Extension runs the bootstrap PowerShell on first boot.
# Equivalent to AWS user_data + EC2's automatic execution at launch.
#
# Custom Script Extension only re-executes on existing VMs when the agent
# observes a different `forceUpdateTag`. Hashing the script content into the
# tag means: edit bootstrap.ps1.tpl → terragrunt apply → script reruns. With
# only `protected_settings` to drift on, the provider would update the resource
# but the agent would skip execution because the tag didn't change.
resource "azurerm_virtual_machine_extension" "bootstrap" {
  count                = var.bootstrap_script != "" ? 1 : 0
  name                 = "${local.name_prefix}-bootstrap"
  virtual_machine_id   = azurerm_windows_virtual_machine.this.id
  publisher            = "Microsoft.Compute"
  type                 = "CustomScriptExtension"
  type_handler_version = "1.10"

  # Custom Script Extension treats `protected_settings` as opaque + sensitive,
  # so a content-only change there doesn't always trigger re-execution. The
  # public `settings` field is observed and replaying a fresh script_hash
  # forces the agent to re-run when the bootstrap content changes.
  settings = jsonencode({
    script_hash = sha256(var.bootstrap_script)
  })

  protected_settings = jsonencode({
    commandToExecute = "powershell.exe -ExecutionPolicy Unrestricted -EncodedCommand ${textencodebase64(var.bootstrap_script, "UTF-16LE")}"
  })

  tags = local.common_tags
}
