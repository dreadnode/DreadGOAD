output "nat_eip" {
  value       = aws_eip.nat.public_ip
  description = "The public IP of the NAT gateway"
}

output "public_subnet_ids" {
  value       = [for subnet in aws_subnet.public : subnet.id]
  description = "The IDs of the public subnets"
}

output "private_subnet_ids" {
  value       = [for subnet in aws_subnet.private : subnet.id]
  description = "The IDs of the private subnets"
}

output "private_subnet_cidrs" {
  value       = [for subnet in aws_subnet.private : subnet.cidr_block]
  description = "The CIDR blocks of the private subnets"
}

output "public_subnet_cidrs" {
  value       = [for subnet in aws_subnet.public : subnet.cidr_block]
  description = "The CIDR blocks of the public subnets"
}

output "vpc_cidr" {
  value       = aws_vpc.main.cidr_block
  description = "The CIDR block used by the VPC"
}

output "vpc_endpoints" {
  description = "Map of VPC endpoint configurations"
  value = {
    for k, v in aws_vpc_endpoint.endpoints : k => {
      service     = var.vpc_endpoints[k].service
      type        = var.vpc_endpoints[k].type
      private_dns = var.vpc_endpoints[k].private_dns
      id          = v.id
      dns_entry   = v.dns_entry
    }
  }
}

output "vpc_id" {
  value       = aws_vpc.main.id
  description = "The ID of the VPC"
}

output "vpce_security_group" {
  value = length(aws_security_group.vpce) > 0 ? {
    id = aws_security_group.vpce[0].id
  } : null
  description = "Security group for VPC endpoints"
}

output "private_route_table_id" {
  value       = aws_route_table.private.id
  description = "The ID of the private route table"
}

################################################################################
# Pod Subnet Outputs (for VPC CNI Custom Networking)
################################################################################

output "pod_subnet_ids" {
  value       = [for subnet in aws_subnet.pod : subnet.id]
  description = "The IDs of the pod subnets (from secondary CIDR)"
}

output "pod_subnet_cidrs" {
  value       = [for subnet in aws_subnet.pod : subnet.cidr_block]
  description = "The CIDR blocks of the pod subnets"
}

output "pod_subnets_by_az" {
  value = {
    for subnet in aws_subnet.pod : subnet.availability_zone => {
      id   = subnet.id
      cidr = subnet.cidr_block
    }
  }
  description = "Map of pod subnets by availability zone"
}

output "secondary_cidr_block" {
  value       = var.secondary_cidr_block
  description = "The secondary CIDR block for pod networking"
}
