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
  description = "Resource group where the Bastion host and related resources are deployed."
  type        = string
}

variable "virtual_network_id" {
  description = "VNet ID the Bastion host is attached to. Required for Developer SKU and used as an output anchor for dedicated SKUs."
  type        = string
  default     = null
}

variable "virtual_network_name" {
  description = "VNet name where the AzureBastionSubnet will be created for dedicated Bastion SKUs."
  type        = string
  default     = null
}

variable "sku" {
  description = "Azure Bastion SKU. Use Standard for native client/tunneling support; Developer is dev/test only and uses shared infrastructure."
  type        = string
  default     = "Standard"

  validation {
    condition     = contains(["Developer", "Basic", "Standard", "Premium"], var.sku)
    error_message = "sku must be one of: Developer, Basic, Standard, Premium."
  }
}

variable "bastion_subnet_id" {
  description = "Existing AzureBastionSubnet ID to use for dedicated Bastion SKUs. If null, the module creates one."
  type        = string
  default     = null
}

variable "bastion_subnet_cidr" {
  description = "CIDR for AzureBastionSubnet when the module creates it. Must be /26 or larger."
  type        = string
  default     = "10.8.2.0/26"

  validation {
    condition     = can(cidrhost(var.bastion_subnet_cidr, 0))
    error_message = "bastion_subnet_cidr must be a valid IPv4 CIDR block."
  }
}

variable "public_ip_id" {
  description = "Existing Standard static public IP to attach to dedicated Bastion SKUs. If null, the module creates one."
  type        = string
  default     = null
}

variable "copy_paste_enabled" {
  description = "Enable copy/paste support in Bastion sessions."
  type        = bool
  default     = true
}

variable "file_copy_enabled" {
  description = "Enable file copy support. Supported only on Standard and Premium."
  type        = bool
  default     = false
}

variable "ip_connect_enabled" {
  description = "Enable IP-based connections. Supported only on Standard and Premium."
  type        = bool
  default     = false
}

variable "kerberos_enabled" {
  description = "Enable Kerberos support. Supported only on Standard and Premium."
  type        = bool
  default     = false
}

variable "shareable_link_enabled" {
  description = "Enable shareable links. Supported only on Standard and Premium."
  type        = bool
  default     = false
}

variable "tunneling_enabled" {
  description = "Enable tunneling/native client support. Supported only on Standard and Premium."
  type        = bool
  default     = false
}

variable "session_recording_enabled" {
  description = "Enable session recording. Supported only on Premium."
  type        = bool
  default     = false
}

variable "scale_units" {
  description = "Scale units for the Bastion host. Standard and Premium support 2-50; Basic and Developer are fixed."
  type        = number
  default     = 2

  validation {
    condition     = var.scale_units >= 2 && var.scale_units <= 50
    error_message = "scale_units must be between 2 and 50."
  }
}

variable "zones" {
  description = "Availability zones for the Bastion host and created public IP. Null leaves the deployment unpinned."
  type        = set(string)
  default     = null
}

variable "additional_tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
