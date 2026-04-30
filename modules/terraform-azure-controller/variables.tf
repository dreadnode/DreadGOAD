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
  description = "Resource group the controller VM and its NIC/NSG/subnet are deployed into."
  type        = string
}

variable "virtual_network_name" {
  description = "VNet name where the controller subnet will be created."
  type        = string
}

variable "controller_subnet_cidr" {
  description = "CIDR for the controller's dedicated subnet. /28 is plenty for one VM."
  type        = string
  default     = "10.8.3.0/28"

  validation {
    condition     = can(cidrhost(var.controller_subnet_cidr, 0))
    error_message = "controller_subnet_cidr must be a valid IPv4 CIDR block."
  }
}

variable "ssh_source_address_prefix" {
  description = "Source allowed to reach the controller on TCP 22. Defaults to the AzureBastionSubnet CIDR — the only intended ingress path."
  type        = string
  default     = "10.8.2.0/26"
}

variable "instance_size" {
  description = "Azure VM size. B2s is the cheapest option that runs Ansible against a full GOAD lab without thrashing."
  type        = string
  default     = "Standard_B2s"
}

variable "admin_username" {
  description = "Local admin username for the controller VM."
  type        = string
  default     = "dreadadmin"
}

variable "admin_ssh_public_key" {
  description = "SSH public key authorised on the controller. When null, the module generates an ephemeral ed25519 keypair and writes the private key to ephemeral_key_output_path."
  type        = string
  default     = null
}

variable "ephemeral_key_output_path" {
  description = "Filesystem path to write the generated private key when admin_ssh_public_key is null. Required in that case; ignored when an explicit public key is supplied."
  type        = string
  default     = null
}

variable "os_disk_size_gb" {
  description = "Size of the OS disk in GB. 32 is enough for Ansible + checkout; bump if you cache lab artefacts on disk."
  type        = number
  default     = 32
}

variable "os_disk_storage_account_type" {
  description = "Storage account type for the OS disk."
  type        = string
  default     = "StandardSSD_LRS"
}

variable "source_image" {
  description = "Marketplace image reference. Defaults to Ubuntu 24.04 LTS (Noble)."
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

variable "ansible_galaxy_collections" {
  description = "Galaxy collections installed at first boot. Override only if you need extra collections beyond the GOAD baseline."
  type        = list(string)
  default = [
    "ansible.windows",
    "community.windows",
    "microsoft.ad",
  ]
}

variable "additional_tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
