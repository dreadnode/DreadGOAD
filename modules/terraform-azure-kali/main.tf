locals {
  name_prefix = "${var.env}-${var.deployment_name}-kali"

  base_tags = {
    Module       = "terraform-azure-kali"
    Environment  = var.env
    ManagedBy    = "Terraform"
    AccessMethod = "BastionSSH"
    Role         = "AttackBox"
  }

  tags = merge(local.base_tags, var.additional_tags)

  generate_ssh_key = var.admin_ssh_public_key == null

  effective_public_key = (
    local.generate_ssh_key
    ? tls_private_key.kali[0].public_key_openssh
    : var.admin_ssh_public_key
  )
}

resource "tls_private_key" "kali" {
  count = local.generate_ssh_key ? 1 : 0

  algorithm = "ED25519"
}

resource "local_sensitive_file" "kali_key" {
  count = local.generate_ssh_key ? 1 : 0

  content         = tls_private_key.kali[0].private_key_openssh
  filename        = var.ephemeral_key_output_path
  file_permission = "0600"

  lifecycle {
    precondition {
      condition     = var.ephemeral_key_output_path != null
      error_message = "ephemeral_key_output_path must be set when admin_ssh_public_key is null."
    }
  }
}

resource "azurerm_subnet" "this" {
  name                 = "${local.name_prefix}-subnet"
  resource_group_name  = var.resource_group_name
  virtual_network_name = var.virtual_network_name
  address_prefixes     = [var.kali_subnet_cidr]
}

resource "azurerm_network_security_group" "this" {
  name                = "${local.name_prefix}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  security_rule {
    name                       = "AllowSSHFromBastion"
    priority                   = 100
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "Tcp"
    source_port_range          = "*"
    destination_port_range     = "22"
    source_address_prefix      = var.ssh_source_address_prefix
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "AllowAzureLoadBalancer"
    priority                   = 110
    direction                  = "Inbound"
    access                     = "Allow"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "AzureLoadBalancer"
    destination_address_prefix = "*"
  }

  security_rule {
    name                       = "DenyAllInbound"
    priority                   = 4096
    direction                  = "Inbound"
    access                     = "Deny"
    protocol                   = "*"
    source_port_range          = "*"
    destination_port_range     = "*"
    source_address_prefix      = "*"
    destination_address_prefix = "*"
  }

  tags = merge(local.tags, { Name = "${local.name_prefix}-nsg" })
}

resource "azurerm_subnet_network_security_group_association" "this" {
  subnet_id                 = azurerm_subnet.this.id
  network_security_group_id = azurerm_network_security_group.this.id
}

resource "azurerm_network_interface" "this" {
  name                = "${local.name_prefix}-nic"
  location            = var.location
  resource_group_name = var.resource_group_name

  ip_configuration {
    name                          = "internal"
    subnet_id                     = azurerm_subnet.this.id
    private_ip_address_allocation = "Dynamic"
  }

  tags = merge(local.tags, { Name = "${local.name_prefix}-nic" })
}

resource "azurerm_linux_virtual_machine" "this" {
  name                = "${local.name_prefix}-vm"
  computer_name       = substr(local.name_prefix, 0, 63)
  resource_group_name = var.resource_group_name
  location            = var.location
  size                = var.instance_size
  admin_username      = var.admin_username

  network_interface_ids = [azurerm_network_interface.this.id]

  disable_password_authentication = true

  admin_ssh_key {
    username   = var.admin_username
    public_key = local.effective_public_key
  }

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

  plan {
    name      = var.plan.name
    product   = var.plan.product
    publisher = var.plan.publisher
  }

  tags = merge(local.tags, { Name = "${local.name_prefix}-vm" })
}
