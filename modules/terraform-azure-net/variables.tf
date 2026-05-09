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

variable "vnet_cidr" {
  description = "CIDR block for the VNet."
  type        = string
  default     = "10.8.0.0/16"
}

variable "private_subnet_cidr" {
  description = "CIDR for the private subnet where lab VMs run."
  type        = string
  default     = "10.8.1.0/24"
}

variable "public_subnet_cidr" {
  description = "CIDR for the public subnet (jumpbox/bastion)."
  type        = string
  default     = "10.8.0.0/24"
}

variable "create_public_subnet" {
  description = "Whether to create a public subnet."
  type        = bool
  default     = false
}

variable "additional_tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
