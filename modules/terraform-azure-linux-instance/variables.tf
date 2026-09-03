variable "env" {
  description = "Environment name."
  type        = string
}

variable "deployment_name" {
  description = "Deployment name used in Azure resource names."
  type        = string
}

variable "instance_name" {
  description = "Logical host name."
  type        = string
}

variable "computer_name" {
  description = "Linux hostname. Defaults to instance_name."
  type        = string
  default     = ""
}

variable "location" {
  description = "Azure region."
  type        = string
}

variable "resource_group_name" {
  description = "Resource group for the VM and its disks and NIC."
  type        = string
}

variable "subnet_id" {
  description = "Existing subnet to attach the VM NIC to."
  type        = string
}

variable "private_ip_address" {
  description = "Optional static private IPv4 address. Dynamic allocation is used when null."
  type        = string
  default     = null

  validation {
    condition     = var.private_ip_address == null || can(cidrhost("${var.private_ip_address}/32", 0))
    error_message = "private_ip_address must be a valid IPv4 address or null."
  }
}

variable "instance_size" {
  description = "Azure VM size."
  type        = string
  default     = "Standard_D2s_v3"
}

variable "admin_username" {
  description = "Linux administrative user."
  type        = string
  default     = "scopeadmin"
}

variable "admin_ssh_public_key" {
  description = "OpenSSH public key authorized for the administrative user."
  type        = string

  validation {
    condition     = length(trimspace(var.admin_ssh_public_key)) > 0
    error_message = "admin_ssh_public_key must not be empty."
  }
}

variable "os_disk_size_gb" {
  description = "OS disk size in GiB."
  type        = number
  default     = 64
}

variable "os_disk_storage_account_type" {
  description = "Azure storage type for the OS disk."
  type        = string
  default     = "StandardSSD_LRS"
}

variable "data_disks" {
  description = "Additional managed data disks keyed by a stable logical name."
  type = map(object({
    lun                  = number
    size_gb              = number
    storage_account_type = optional(string, "StandardSSD_LRS")
    caching              = optional(string, "ReadWrite")
  }))
  default = {}

  validation {
    condition     = length(distinct([for disk in values(var.data_disks) : disk.lun])) == length(var.data_disks)
    error_message = "Each data disk must use a unique LUN."
  }
}

variable "source_image" {
  description = "Marketplace image reference; ignored when source_image_id is set."
  type = object({
    publisher = string
    offer     = string
    sku       = string
    version   = string
  })
  default = {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }
}

variable "source_image_id" {
  description = "Optional managed or Compute Gallery image resource ID."
  type        = string
  default     = null
}

variable "plan" {
  description = "Optional Marketplace plan associated with the selected image."
  type = object({
    name      = string
    product   = string
    publisher = string
  })
  default = null
}

variable "custom_data" {
  description = "Cloud-init content. Azure receives it base64 encoded."
  type        = string
  default     = ""
  sensitive   = true
}

variable "tags" {
  description = "Additional tags applied to all resources."
  type        = map(string)
  default     = {}
}
