locals {
  base_tags = {
    Project     = "DreadGOAD"
    Environment = var.env
    OS          = "Linux"
    Module      = "terraform-aws-linux-instance"
  }

  tags = merge(var.additional_tags, local.base_tags)
}

module "instance" {
  source = "../terraform-aws-instance-factory"

  env           = var.env
  instance_name = "${var.deployment_name}-${var.instance_name}"
  instance_type = var.instance_type
  os_type       = "linux"
  enable_asg    = false

  vpc_id     = var.vpc_id
  subnet_id  = var.subnet_id
  private_ip = var.private_ip

  enable_ssm       = true
  assign_public_ip = false
  user_data        = var.user_data

  linux_ami_owners = ["099720109477"]
  additional_linux_ami_filters = [
    {
      name   = "name"
      values = ["ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*"]
    },
  ]

  ingress_rules = [
    {
      description = "Allow all traffic inside the GOAT VPC"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = [var.vpc_cidr]
    },
  ]

  egress_rules = [
    {
      description = "Allow package, image, and service egress through the NAT gateway"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = ["0.0.0.0/0"]
    },
  ]

  enable_monitoring = true
  enable_metadata   = true
  require_imdsv2    = true
  encrypt_volumes   = true
  root_volume_size  = var.root_volume_size
  volume_type       = "gp3"

  additional_ebs_volumes = var.data_volume_size > 0 ? [
    {
      device_name           = "/dev/sdf"
      volume_size           = var.data_volume_size
      volume_type           = "gp3"
      delete_on_termination = true
    },
  ] : []

  tags = local.tags
}
