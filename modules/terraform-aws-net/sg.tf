# Ensure that the default security group does not allow unrestricted ingress or egress traffic
resource "aws_default_security_group" "default" {
  vpc_id = aws_vpc.main.id
}

resource "aws_security_group" "vpce" {
  # checkov:skip=CKV2_AWS_5: Ensure that Security Groups are attached to another resource
  # checkov:skip=CKV_AWS_382: Ensure no security groups allow egress from 0.0.0.0:0 to port -1
  count       = length(var.vpc_endpoints) > 0 ? 1 : 0
  name        = local.vpce_sg_name
  description = "Security group for VPC endpoints"
  vpc_id      = aws_vpc.main.id

  dynamic "ingress" {
    for_each = var.vpce_security_group_rules.ingress_security_group_ids
    content {
      description     = "HTTPS from allowed security groups"
      from_port       = 443
      to_port         = 443
      protocol        = "tcp"
      security_groups = [ingress.value]
    }
  }

  dynamic "ingress" {
    for_each = length(var.vpce_security_group_rules.ingress_cidr_blocks) > 0 ? [1] : []
    content {
      description = "HTTPS from CIDR blocks"
      from_port   = 443
      to_port     = 443
      protocol    = "tcp"
      cidr_blocks = var.vpce_security_group_rules.ingress_cidr_blocks
    }
  }

  # Add self-referential ingress rule
  ingress {
    description = "Allow traffic between endpoints"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    self        = true
  }

  egress {
    description = "Allow all outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = var.vpce_security_group_rules.egress_cidr_blocks
  }

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = merge({ Name = local.vpce_sg_name }, local.tags)
}
