locals {
  name_prefix = "${var.env}-${var.deployment_name}-controller"

  base_tags = {
    Module       = "terraform-azure-controller"
    Environment  = var.env
    ManagedBy    = "Terraform"
    AccessMethod = "BastionSSH"
    Role         = "AnsibleController"
  }

  tags = merge(local.base_tags, var.additional_tags)

  cloud_init = templatefile("${path.module}/cloud-init.yaml.tpl", {
    admin_user  = var.admin_username
    collections = var.ansible_galaxy_collections
  })

  # Generate an ephemeral keypair when the caller didn't supply one. The
  # privkey lands on the operator's machine via local_sensitive_file; the
  # pubkey gets injected into authorized_keys on the VM.
  generate_ssh_key = var.admin_ssh_public_key == null

  effective_public_key = (
    local.generate_ssh_key
    ? tls_private_key.controller[0].public_key_openssh
    : var.admin_ssh_public_key
  )
}

resource "tls_private_key" "controller" {
  count = local.generate_ssh_key ? 1 : 0

  algorithm = "ED25519"
}

resource "local_sensitive_file" "controller_key" {
  count = local.generate_ssh_key ? 1 : 0

  content         = tls_private_key.controller[0].private_key_openssh
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
  address_prefixes     = [var.controller_subnet_cidr]
}

resource "azurerm_network_security_group" "this" {
  name                = "${local.name_prefix}-nsg"
  location            = var.location
  resource_group_name = var.resource_group_name

  # Only Bastion-originated SSH is allowed. Bastion brokers the tunnel from
  # the operator's laptop, so the controller never sees the public internet.
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

  custom_data = base64encode(local.cloud_init)

  identity {
    type = "SystemAssigned"
  }

  tags = merge(local.tags, { Name = "${local.name_prefix}-vm" })
}
