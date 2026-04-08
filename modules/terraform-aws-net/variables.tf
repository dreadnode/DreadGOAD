variable "additional_tags" {
  type        = map(string)
  description = "Additional tags to apply to resources"
  default     = {}
}

variable "deployment_name" {
  type        = string
  description = "Name of the deployment (ex: \"crucible\")"
}

variable "env" {
  description = "The environment name (e.g., dev, staging, prod, global)"
  type        = string
}

variable "kubernetes_tags" {
  description = "Configuration for Kubernetes integration tags"
  type = object({
    enabled                    = bool
    cluster_name               = optional(string, "") # Will default to {env}-{deployment_name} if empty
    enable_karpenter_discovery = optional(bool, false)
  })
  default = {
    enabled                    = false
    enable_karpenter_discovery = false
  }
}

variable "map_public_ip" {
  type        = bool
  description = "Map public IP addresses to new instances."
  default     = true
}

variable "vpc_cidr_block" {
  type        = string
  description = "Top-level CIDR block for the VPC"
  default     = "10.0.0.0/16"
}

variable "vpc_endpoints" {
  description = "Map of VPC endpoint configurations"
  type = map(object({
    service     = string
    type        = string
    private_dns = optional(bool, false) # Make private_dns optional with default false
  }))
  default = {
    secretsmanager = {
      service     = "secretsmanager"
      type        = "Interface"
      private_dns = true
    }
    ecr_dkr = {
      service     = "ecr.dkr"
      type        = "Interface"
      private_dns = true
    }
    ecr_api = {
      service     = "ecr.api"
      type        = "Interface"
      private_dns = true
    }
    cloudwatch = {
      service     = "logs"
      type        = "Interface"
      private_dns = true
    }
    sns = {
      service     = "sns"
      type        = "Interface"
      private_dns = false
    }
    s3 = {
      service = "s3"
      type    = "Gateway"
      # private_dns not required for Gateway endpoints - will default to false
    }
  }

  validation {
    condition     = alltrue([for v in var.vpc_endpoints : contains(["Interface", "Gateway"], v.type)])
    error_message = "VPC endpoint type must be either 'Interface' or 'Gateway'."
  }
}

variable "vpce_security_group_rules" {
  description = "Security group rules for VPC endpoints"
  type = object({
    ingress_security_group_ids = optional(list(string), [])
    ingress_cidr_blocks        = optional(list(string), [])
    egress_cidr_blocks         = optional(list(string), ["0.0.0.0/0"])
  })

  default = {
    ingress_security_group_ids = []
    ingress_cidr_blocks        = []
    egress_cidr_blocks         = ["0.0.0.0/0"]
  }
}

variable "additional_private_routes" {
  description = "Additional routes to add to the private route table"
  type = map(object({
    destination_cidr_block    = string
    network_interface_id      = optional(string, null)
    gateway_id                = optional(string, null)
    nat_gateway_id            = optional(string, null)
    transit_gateway_id        = optional(string, null)
    vpc_peering_connection_id = optional(string, null)
  }))
  default = {}
}

variable "secondary_cidr_block" {
  type        = string
  description = "Secondary CIDR block for pod networking (e.g., 100.64.0.0/16). Uses CG-NAT space to avoid conflicts."
  default     = ""
}

variable "pod_subnet_newbits" {
  type        = number
  description = "Number of bits to add to the secondary CIDR for pod subnets (e.g., 4 for /20 subnets from /16)"
  default     = 4
}
