variable "name" {
  description = "Logical name used to derive the peering resource names. Both directions get suffixes (-to-remote and -from-remote)."
  type        = string
}

variable "local_resource_group_name" {
  description = "Resource group of the local VNet (the side this stack 'owns' first-class)."
  type        = string
}

variable "local_virtual_network_name" {
  description = "Name of the local VNet."
  type        = string
}

variable "remote_resource_group_name" {
  description = "Resource group of the remote VNet."
  type        = string
}

variable "remote_virtual_network_name" {
  description = "Name of the remote VNet. The module looks it up via data source so the caller doesn't need to plumb the full ID."
  type        = string
}

variable "allow_forwarded_traffic" {
  description = "Whether traffic forwarded from outside (e.g. via NVA) is allowed across the peering. GOAD attacker traffic is sourced directly from peered subnets, so default off."
  type        = bool
  default     = false
}

variable "allow_gateway_transit" {
  description = "Whether the local VNet allows the remote VNet to use its gateway. Lab peerings don't share gateways."
  type        = bool
  default     = false
}

variable "use_remote_gateways" {
  description = "Whether the local VNet uses the remote VNet's gateway. Mutually exclusive with allow_gateway_transit on the same side."
  type        = bool
  default     = false
}

variable "remote_nsg_name" {
  description = "Optional NSG on the remote side to open for ingress from local CIDRs. The remote NSG's default DenyAllInbound means peered traffic is dropped unless explicitly allowed; set this to the GOAD/lab private-subnet NSG when peering an attacker workstation in. Null = skip."
  type        = string
  default     = null
}

variable "remote_nsg_resource_group_name" {
  description = "Resource group of the remote NSG. Defaults to remote_resource_group_name (typical case: NSG sits in the same RG as the remote VNet)."
  type        = string
  default     = null
}

variable "remote_inbound_allow_cidrs" {
  description = "CIDRs on the local side that should be allowed inbound on the remote NSG. Ignored unless remote_nsg_name is set. Adds one allow rule per entry."
  type        = list(string)
  default     = []
}

variable "remote_inbound_priority_base" {
  description = "Starting NSG rule priority for the generated allow rules. Each subsequent CIDR increments by 1. Pick a band that doesn't collide with existing rules."
  type        = number
  default     = 200
}
