variable "deployment_name" {
  description = "Name of the deployment (e.g. \"goad\")."
  type        = string
}

variable "env" {
  description = "Environment name (e.g. test, staging)."
  type        = string
}

variable "location" {
  description = "Azure region."
  type        = string
}

variable "resource_group_name" {
  description = "Resource group the Kali VM and its NIC/NSG/subnet are deployed into."
  type        = string
}

variable "virtual_network_name" {
  description = "VNet name where the Kali subnet will be created."
  type        = string
}

variable "kali_subnet_cidr" {
  description = "CIDR for the Kali attack box's dedicated subnet. /28 is plenty for one VM."
  type        = string
  default     = "10.8.4.0/28"

  validation {
    condition     = can(cidrhost(var.kali_subnet_cidr, 0))
    error_message = "kali_subnet_cidr must be a valid IPv4 CIDR block."
  }
}

variable "ssh_source_address_prefix" {
  description = "Source allowed to reach the Kali box on TCP 22. Defaults to the AzureBastionSubnet CIDR."
  type        = string
  default     = "10.8.2.0/26"
}

variable "instance_size" {
  description = "Azure VM size. D2s_v3 (2 vCPU, 8 GB) handles concurrent attack tooling comfortably."
  type        = string
  default     = "Standard_D2s_v3"
}

variable "admin_username" {
  description = "Local admin username for the Kali VM."
  type        = string
  default     = "kali"
}

variable "admin_ssh_public_key" {
  description = "SSH public key authorised on the Kali VM. When null, the module generates an ephemeral ed25519 keypair and writes the private key to ephemeral_key_output_path."
  type        = string
  default     = null
}

variable "ephemeral_key_output_path" {
  description = "Filesystem path to write the generated private key when admin_ssh_public_key is null."
  type        = string
  default     = null
}

variable "os_disk_size_gb" {
  description = "Size of the OS disk in GB. 32 is enough for stock Kali tooling."
  type        = number
  default     = 32
}

variable "os_disk_storage_account_type" {
  description = "Storage account type for the OS disk."
  type        = string
  default     = "StandardSSD_LRS"
}

variable "source_image" {
  description = "Marketplace image reference. Defaults to the latest Kali Linux release."
  type = object({
    publisher = string
    offer     = string
    sku       = string
    version   = string
  })
  default = {
    publisher = "kali-linux"
    offer     = "kali"
    sku       = "kali-2026-2"
    version   = "latest"
  }
}

variable "plan" {
  description = "Marketplace plan terms for the Kali image. Must be accepted per-subscription via `az vm image terms accept --publisher kali-linux --offer kali --plan <sku>`."
  type = object({
    name      = string
    product   = string
    publisher = string
  })
  default = {
    name      = "kali-2026-2"
    product   = "kali"
    publisher = "kali-linux"
  }
}

variable "additional_tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
