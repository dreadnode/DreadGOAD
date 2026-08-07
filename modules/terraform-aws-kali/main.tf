locals {
  base_tags = {
    Module      = "terraform-aws-kali"
    Project     = "DreadGOAD"
    Environment = var.env
    Role        = "AttackBox"
    Lab         = "${var.deployment_name}-goad"
  }

  # Discovery-critical tags cannot be replaced by caller-supplied metadata.
  tags = merge(var.additional_tags, local.base_tags)

  ami_filters = var.ami_id != "" ? [
    {
      name   = "image-id"
      values = [var.ami_id]
    }
    ] : [
    {
      name   = "name"
      values = [var.ami_name_pattern]
    }
  ]
}

module "kali" {
  source = "../terraform-aws-instance-factory"

  env           = var.env
  instance_name = "${var.deployment_name}-dreadgoad-kali"
  instance_type = var.instance_type
  os_type       = "linux"
  enable_asg    = false

  vpc_id    = var.vpc_id
  subnet_id = var.subnet_id

  # Private by default. A public IP can be explicitly enabled for standalone
  # smoke tests in a public subnet without NAT or VPC endpoints.
  assign_public_ip = var.assign_public_ip

  # The instance factory creates the IAM role/profile with
  # AmazonSSMManagedInstanceCore. The Kali image needs the agent installed by
  # user data before it can register with Systems Manager.
  enable_ssm = true
  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    aws_region = data.aws_region.current.region
  })

  linux_ami_owners             = var.ami_owners
  additional_linux_ami_filters = local.ami_filters

  ingress_rules = [
    {
      description = "Allow lab traffic from the VPC"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = [var.vpc_cidr]
    }
  ]

  egress_rules = [
    {
      description = "Allow outbound traffic"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = ["0.0.0.0/0"]
    }
  ]

  enable_monitoring = true
  enable_metadata   = true
  require_imdsv2    = true
  encrypt_volumes   = true
  root_volume_size  = var.root_volume_size
  volume_type       = "gp3"

  tags = local.tags
}

data "aws_region" "current" {}
