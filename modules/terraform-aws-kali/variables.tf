variable "deployment_name" {
  description = "Name of the deployment (for example, goad)."
  type        = string
}

variable "env" {
  description = "Environment name (for example, test or staging)."
  type        = string
}

variable "vpc_id" {
  description = "VPC in which to deploy the attack box."
  type        = string
}

variable "vpc_cidr" {
  description = "Lab VPC CIDR. Traffic from this range may reach the attack box."
  type        = string
}

variable "subnet_id" {
  description = "Private subnet in which to deploy the attack box."
  type        = string
}

variable "private_ip" {
  description = "Optional fixed private IPv4 address for the attack box."
  type        = string
  default     = null

  validation {
    condition     = var.private_ip == null || can(cidrnetmask("${var.private_ip}/32"))
    error_message = "private_ip must be a valid IPv4 address or null."
  }
}

variable "instance_name" {
  description = "Optional logical instance name. The existing dreadgoad-kali name remains the default."
  type        = string
  default     = ""
}

variable "lab_name" {
  description = "Lab tag value. Defaults to the historical <deployment>-goad value."
  type        = string
  default     = ""
}

variable "instance_type" {
  description = "EC2 instance type for the Kali attack box."
  type        = string
  default     = "t3.medium"
}

variable "root_volume_size" {
  description = "Kali root volume size in GiB."
  type        = number
  default     = 80

  validation {
    condition     = var.root_volume_size >= 25
    error_message = "root_volume_size must be at least 25 GiB for the official Kali Marketplace AMI."
  }
}

variable "ami_name_pattern" {
  description = "Official Kali Marketplace AMI pattern, constrained to Kali product ID 804fcc46-63fc-4eb6-85a1-50e66d6c7215. Ignored when ami_id is set."
  type        = string
  default     = "*kali-last-snapshot-amd64-*-804fcc46-63fc-4eb6-85a1-50e66d6c7215"
}

variable "ami_id" {
  description = "Optional explicit Kali AMI ID override."
  type        = string
  default     = ""
}

variable "ami_owners" {
  description = "Allowed AMI owners. The default is further constrained by ami_name_pattern to the official Kali Marketplace product ID."
  type        = list(string)
  default     = ["aws-marketplace"]
}

variable "additional_tags" {
  description = "Additional tags to merge into attack-box resources."
  type        = map(string)
  default     = {}
}
