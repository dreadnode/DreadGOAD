variable "env" {
  description = "Environment name (e.g. test, staging)."
  type        = string
}

variable "instance_name" {
  description = "Logical instance name; used to derive Azure resource names. Will be lowercased and slug-safe."
  type        = string
}

variable "computer_name" {
  description = "Windows computer name (NetBIOS, max 15 chars). Defaults to first 15 chars of instance_name."
  type        = string
  default     = ""
}

variable "instance_size" {
  description = "Azure VM size."
  type        = string
  default     = "Standard_D2s_v3"
}

variable "resource_group_name" {
  description = "Resource group the VM and NIC are deployed into."
  type        = string
}

variable "location" {
  description = "Azure region."
  type        = string
}

variable "subnet_id" {
  description = "Subnet the VM's NIC attaches to."
  type        = string
}

variable "assign_public_ip" {
  description = "Assign a public IP to the VM (typically false; outbound is via NAT gateway)."
  type        = bool
  default     = false
}

variable "admin_username" {
  description = "Local admin username."
  type        = string
  default     = "goadadmin"
}

variable "admin_password" {
  description = "Local admin password (single source-of-truth from lab config)."
  type        = string
  sensitive   = true
}

variable "os_disk_size_gb" {
  description = "Size of the OS disk in GB. Must be >= the size baked into source_image (127 GB for Windows Server 2022 Datacenter Azure Edition)."
  type        = number
  default     = 128
}

variable "os_disk_storage_account_type" {
  description = "Storage account type for the OS disk."
  type        = string
  default     = "StandardSSD_LRS"
}

variable "source_image" {
  description = "Marketplace image reference."
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

variable "bootstrap_script" {
  description = "PowerShell script executed on first boot via Custom Script Extension. Empty string disables bootstrap."
  type        = string
  default     = ""
  sensitive   = true
}

variable "tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
