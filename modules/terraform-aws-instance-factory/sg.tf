locals {
  alb_allowed_cidrs = var.include_current_ip ? concat(var.allowed_cidr_blocks, ["${chomp(data.http.current_ip[0].response_body)}/32"]) : var.allowed_cidr_blocks

  default_egress_rule = [{
    name        = "Allow All Outbound"
    description = "Allow all outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }]

  # Combine SSH rules with other ingress rules if SSH is enabled
  ssh_rule = (var.ssh_public_key != "" && !var.enable_ssm && length(var.ssh_allowed_cidr_blocks) > 0) ? [{
    name        = "SSH Access"
    description = "Allow SSH access"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidr_blocks
  }] : []

  instance_base_ingress_rules = concat(local.ssh_rule, var.ingress_rules)
  final_egress_rules          = length(var.egress_rules) > 0 ? var.egress_rules : local.default_egress_rule
}

resource "aws_security_group" "alb" {
  count = local.create_alb ? 1 : 0

  name        = "${local.deployment_name}-alb-sg"
  description = "Security group for ALB"
  vpc_id      = var.vpc_id

  # Allow HTTPS/HTTP from the specified CIDR blocks
  dynamic "ingress" {
    for_each = var.tls_configuration != null ? [443] : [80]
    content {
      description = "${local.deployment_name}-alb-${ingress.value == 443 ? "https" : "http"}-ingress"
      from_port   = ingress.value
      to_port     = ingress.value
      protocol    = "tcp"
      cidr_blocks = local.alb_allowed_cidrs
    }
  }

  # This covers all private IP ranges used in AWS VPCs
  dynamic "ingress" {
    for_each = var.alb_internal ? (var.tls_configuration != null ? [443] : [80]) : []
    content {
      description = "Allow internal network access to ALB ${ingress.value == 443 ? "HTTPS" : "HTTP"}"
      from_port   = ingress.value
      to_port     = ingress.value
      protocol    = "tcp"
      cidr_blocks = ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"] # Standard private IP ranges
    }
  }

  # Additional security group rules for ALB
  dynamic "ingress" {
    for_each = var.alb_additional_security_group_rules
    content {
      description      = lookup(ingress.value, "description", null)
      from_port        = lookup(ingress.value, "from_port", null)
      to_port          = lookup(ingress.value, "to_port", null)
      protocol         = lookup(ingress.value, "protocol", null)
      cidr_blocks      = lookup(ingress.value, "cidr_blocks", null)
      ipv6_cidr_blocks = lookup(ingress.value, "ipv6_cidr_blocks", null)
      prefix_list_ids  = lookup(ingress.value, "prefix_list_ids", null)
      security_groups  = lookup(ingress.value, "security_groups", null)
      self             = lookup(ingress.value, "self", null)
    }
  }

  egress {
    description = "${local.deployment_name}-alb-to-instances"
    from_port   = var.alb_target_port
    to_port     = var.alb_target_port
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"] # This will be restricted by the instance security group
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.deployment_name}-alb-sg"
    }
  )
}

resource "aws_security_group" "this" {
  name        = "${local.deployment_name}-sg"
  description = "Security group for ${var.os_type} instance(s)"
  vpc_id      = var.vpc_id

  # First, create all the base ingress rules
  dynamic "ingress" {
    for_each = local.instance_base_ingress_rules
    content {
      description      = lookup(ingress.value, "description", "")
      from_port        = lookup(ingress.value, "from_port", null)
      to_port          = lookup(ingress.value, "to_port", null)
      protocol         = lookup(ingress.value, "protocol", null)
      cidr_blocks      = lookup(ingress.value, "cidr_blocks", null)
      ipv6_cidr_blocks = lookup(ingress.value, "ipv6_cidr_blocks", null)
      prefix_list_ids  = lookup(ingress.value, "prefix_list_ids", null)
      security_groups  = lookup(ingress.value, "security_groups", null)
      self             = lookup(ingress.value, "self", null)
    }
  }

  # Add the ALB rule separately if ALB is enabled
  dynamic "ingress" {
    for_each = local.create_alb ? [1] : []
    content {
      description     = "Allow traffic from ALB to instances"
      from_port       = var.alb_target_port
      to_port         = var.alb_target_port
      protocol        = "tcp"
      security_groups = [aws_security_group.alb[0].id]
    }
  }

  # Egress rules
  dynamic "egress" {
    for_each = local.final_egress_rules
    content {
      description      = lookup(egress.value, "description", "")
      from_port        = lookup(egress.value, "from_port", null)
      to_port          = lookup(egress.value, "to_port", null)
      protocol         = lookup(egress.value, "protocol", null)
      cidr_blocks      = lookup(egress.value, "cidr_blocks", null)
      ipv6_cidr_blocks = lookup(egress.value, "ipv6_cidr_blocks", null)
      prefix_list_ids  = lookup(egress.value, "prefix_list_ids", null)
      security_groups  = lookup(egress.value, "security_groups", null)
      self             = lookup(egress.value, "self", null)
    }
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.deployment_name}-sg"
    }
  )
}
