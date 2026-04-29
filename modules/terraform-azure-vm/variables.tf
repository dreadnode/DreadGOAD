variable "address_space" {
  description = "VNet CIDR block."
  type        = string
  default     = "10.40.0.0/16"
}

variable "admin_username" {
  description = "Local admin username on the Windows VM."
  type        = string
  default     = "goadadmin"
}

variable "location" {
  description = "Azure region (e.g. eastus)."
  type        = string
}

variable "os_disk_storage_account_type" {
  description = "Storage account type for the OS disk."
  type        = string
  default     = "StandardSSD_LRS"
}

variable "source_image" {
  description = "Marketplace image reference for the Windows VM."
  type = object({
    publisher = string
    offer     = string
    sku       = string
    version   = string
  })
  default = {
    publisher = "MicrosoftWindowsServer"
    offer     = "WindowsServer"
    sku       = "2022-datacenter-azure-edition"
    version   = "latest"
  }
}

variable "subnet_cidr" {
  description = "Subnet CIDR inside the VNet."
  type        = string
  default     = "10.40.1.0/24"
}

variable "tags" {
  description = "Additional tags applied to every resource."
  type        = map(string)
  default     = {}
}

variable "vm_name" {
  description = "Name of the VM (used to derive resource names)."
  type        = string
}

variable "vm_size" {
  description = "Azure VM size."
  type        = string
  default     = "Standard_B2s"
}
