variable "env" {
  description = "Environment name."
  type        = string
}

variable "deployment_name" {
  description = "Deployment component used in resource names."
  type        = string
}

variable "instance_name" {
  description = "Logical Linux host name."
  type        = string
}

variable "vpc_id" {
  description = "VPC in which to create the instance."
  type        = string
}

variable "vpc_cidr" {
  description = "VPC CIDR allowed to reach the range host."
  type        = string
}

variable "subnet_id" {
  description = "Private subnet in which to create the instance."
  type        = string
}

variable "private_ip" {
  description = "Fixed private IPv4 address for the instance."
  type        = string

  validation {
    condition     = can(cidrnetmask("${var.private_ip}/32"))
    error_message = "private_ip must be a valid IPv4 address."
  }
}

variable "instance_type" {
  description = "EC2 instance type."
  type        = string
  default     = "m6i.xlarge"
}

variable "root_volume_size" {
  description = "Encrypted root volume size in GiB."
  type        = number
  default     = 64
}

variable "data_volume_size" {
  description = "Encrypted range data volume size in GiB; zero disables it."
  type        = number
  default     = 100

  validation {
    condition     = var.data_volume_size == 0 || var.data_volume_size >= 20
    error_message = "data_volume_size must be zero or at least 20 GiB."
  }
}

variable "user_data" {
  description = "Cloud-init content used to establish the hostname and SSM agent."
  type        = string
  default     = ""
  sensitive   = true
}

variable "additional_tags" {
  description = "Additional metadata applied to the instance and its resources."
  type        = map(string)
  default     = {}
}
