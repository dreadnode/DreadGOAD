locals {
  name_prefix = "${var.env}-${var.deployment_name}"
  az_names    = data.aws_availability_zones.available.names

  eip_name = "${local.name_prefix}-nat-eip"
  igw_name = local.name_prefix

  # Cluster name determination for k8s tags
  k8s_cluster_name = var.kubernetes_tags.enabled ? (
    coalesce(var.kubernetes_tags.cluster_name, "${var.env}-${var.deployment_name}")
  ) : ""

  # K8s-specific tags to apply conditionally
  k8s_tags = var.kubernetes_tags.enabled ? {
    "kubernetes.io/cluster/${local.k8s_cluster_name}" = "owned"
  } : {}

  # Karpenter discovery tags for private subnets
  karpenter_tags = var.kubernetes_tags.enabled && var.kubernetes_tags.enable_karpenter_discovery ? {
    "karpenter.sh/discovery" = local.k8s_cluster_name
  } : {}
  nat_name                   = local.name_prefix
  private_route_table_name   = local.name_prefix
  public_route_table_name    = local.name_prefix
  private_subnet_name_prefix = "${local.name_prefix}-private"
  public_subnet_name_prefix  = "${local.name_prefix}-public"

  subnet_newbits = 8 # This will create /24 subnets from a /16 VPC

  ## Create subnet to AZ mapping with calculated CIDRs
  public_subnet_configs = {
    for idx in range(length(local.az_names)) : idx => {
      cidr = cidrsubnet(var.vpc_cidr_block, local.subnet_newbits, idx)
      az   = local.az_names[idx]
    }
  }

  private_subnet_configs = {
    for idx in range(length(local.az_names)) : idx => {
      cidr = cidrsubnet(var.vpc_cidr_block, local.subnet_newbits, idx + length(local.az_names))
      az   = local.az_names[idx]
    }
  }

  # Pod subnets from secondary CIDR (for VPC CNI custom networking)
  pod_subnet_configs = var.secondary_cidr_block != "" ? {
    for idx in range(length(local.az_names)) : idx => {
      cidr = cidrsubnet(var.secondary_cidr_block, var.pod_subnet_newbits, idx)
      az   = local.az_names[idx]
    }
  } : {}

  pod_subnet_name_prefix = "${local.name_prefix}-pod"

  subnet_by_az = {
    for subnet in aws_subnet.private :
    subnet.availability_zone => subnet.id...
  }

  unique_az_subnets = [
    for az, subnets in local.subnet_by_az :
    subnets[0]
  ]

  vpc_name      = local.name_prefix
  vpc_endpoints = var.vpc_endpoints
  vpce_sg_name  = local.name_prefix

  base_tags = {
    Module = "terraform-aws-network"
    Name   = local.name_prefix
  }

  # Merge all tags together
  tags = merge(
    local.base_tags,
    local.k8s_tags,
    var.additional_tags
  )
}

# Rest of your resources remain the same, but update the tags references...

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    { Name = local.igw_name }
  )
}

resource "aws_eip" "nat" {
  domain = "vpc"

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    { Name = local.eip_name }
  )
}

resource "aws_nat_gateway" "main" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public["0"].id

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    { Name = local.nat_name }
  )
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    { Name = local.public_route_table_name }
  )
}

resource "aws_route_table_association" "public" {
  for_each = aws_subnet.public

  route_table_id = aws_route_table.public.id
  subnet_id      = each.value.id
}

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all, route]
  }

  tags = merge(
    local.tags,
    { Name = local.private_route_table_name }
  )
}

# Default NAT gateway route for private subnets
resource "aws_route" "private_nat_gateway" {
  route_table_id         = aws_route_table.private.id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.main.id
}

resource "aws_route_table_association" "private" {
  for_each = aws_subnet.private

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}

resource "aws_route" "additional_private" {
  for_each = var.additional_private_routes

  route_table_id            = aws_route_table.private.id
  destination_cidr_block    = each.value.destination_cidr_block
  network_interface_id      = each.value.network_interface_id
  gateway_id                = each.value.gateway_id
  nat_gateway_id            = each.value.nat_gateway_id
  transit_gateway_id        = each.value.transit_gateway_id
  vpc_peering_connection_id = each.value.vpc_peering_connection_id
}

resource "aws_subnet" "public" {
  # checkov:skip=CKV_AWS_130: "Public IP mappings for public subnets dictated by variable"
  for_each                = local.public_subnet_configs
  availability_zone       = each.value.az
  cidr_block              = each.value.cidr
  vpc_id                  = aws_vpc.main.id
  map_public_ip_on_launch = var.map_public_ip

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    {
      Name = "${local.public_subnet_name_prefix}-${each.key}"
      Type = "public"
    },
    var.kubernetes_tags.enabled ? { "kubernetes.io/role/elb" = "1" } : {}
  )
}

resource "aws_subnet" "private" {
  for_each                = local.private_subnet_configs
  availability_zone       = each.value.az
  cidr_block              = each.value.cidr
  map_public_ip_on_launch = false
  vpc_id                  = aws_vpc.main.id

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    {
      Name = "${local.private_subnet_name_prefix}-${each.key}"
      Type = "private"
    },
    var.kubernetes_tags.enabled ? { "kubernetes.io/role/internal-elb" = "1" } : {},
    local.karpenter_tags
  )
}

resource "aws_vpc" "main" {
  # checkov:skip=CKV2_AWS_11: "Opting out of VPC flow logging as a requirement cause it gets expensive and lacks flexibility"
  cidr_block           = var.vpc_cidr_block
  enable_dns_hostnames = true
  enable_dns_support   = true

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge(
    local.tags,
    { Name = local.vpc_name }
  )
}

################################################################################
# Secondary CIDR and Pod Subnets (for VPC CNI Custom Networking)
################################################################################

resource "aws_vpc_ipv4_cidr_block_association" "secondary" {
  count = var.secondary_cidr_block != "" ? 1 : 0

  vpc_id     = aws_vpc.main.id
  cidr_block = var.secondary_cidr_block
}

resource "aws_subnet" "pod" {
  for_each = local.pod_subnet_configs

  availability_zone       = each.value.az
  cidr_block              = each.value.cidr
  map_public_ip_on_launch = false
  vpc_id                  = aws_vpc.main.id

  # Ensure secondary CIDR is associated before creating subnets
  depends_on = [aws_vpc_ipv4_cidr_block_association.secondary]

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  # NOTE: Pod subnets intentionally do NOT have karpenter.sh/discovery tags
  # Karpenter should launch nodes in PRIMARY private subnets, not pod subnets
  # The VPC CNI uses ENIConfig to assign pod IPs from these subnets
  tags = merge(
    local.tags,
    {
      Name = "${local.pod_subnet_name_prefix}-${each.key}"
      Type = "pod"
    }
  )
}

resource "aws_route_table_association" "pod" {
  for_each = aws_subnet.pod

  subnet_id      = each.value.id
  route_table_id = aws_route_table.private.id
}
