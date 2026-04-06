# =============================================================================
# DC02 - Domain Controller for North Subdomain
# AMI: Built from warpgate-templates/goad-dc-base (Windows Server 2019)
# =============================================================================

include "host" {
  path   = find_in_parent_folders("host.hcl")
  expose = true
}

locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  aws_region      = local.region_vars.locals.aws_region
  deployment_name = local.env_vars.locals.deployment_name

  hostname      = include.host.locals.computer_name
  friendly_name = include.host.locals.hostname
  domain        = include.host.locals.domain
  os_type       = include.host.locals.os_type
  role          = include.host.locals.role
  goad_id       = include.host.locals.goad_id

  # Read admin password from GOAD lab config (single source of truth)
  lab_config     = jsondecode(file("${get_repo_root()}/ad/GOAD/data/${local.env}-config.json"))
  admin_password = local.lab_config.lab.hosts[local.goad_id].local_admin_password
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-instance-factory"
}

dependency "network" {
  config_path = "../../network"
  mock_outputs = {
    vpc_id             = "vpc-mock"
    vpc_cidr           = "10.0.0.0/16"
    private_subnet_ids = ["subnet-mock"]
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env           = local.env
  instance_name = "${local.deployment_name}-dreadgoad-${local.hostname}"
  instance_type = "t3.medium"
  os_type       = local.os_type
  enable_asg    = false
  subnet_id     = dependency.network.outputs.private_subnet_ids[0]
  vpc_id        = dependency.network.outputs.vpc_id

  enable_ssm = true

  additional_iam_policies = {
    cloudwatch_agent = "arn:aws:iam::aws:policy/CloudWatchAgentServerPolicy"
    s3_full_access   = "arn:aws:iam::aws:policy/AmazonS3FullAccess"
  }

  # Windows AMI - uses latest warpgate-built AMI (most_recent = true, owners = ["self"])
  windows_os         = "Windows_Server"
  windows_os_version = "2019-English-Full-Base"
  windows_ami_owners = ["self"]

  additional_windows_ami_filters = [
    {
      name   = "name"
      values = ["goad-dc-base-*"]  # warpgate-templates/goad-dc-base
    }
  ]

  ingress_rules = [
    {
      description = "Allow all traffic from VPC CIDR"
      from_port   = 0
      to_port     = 0
      protocol    = "-1"
      cidr_blocks = [dependency.network.outputs.vpc_cidr]
    },
  ]

  egress_rules = [
    {
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
  root_volume_size  = 100
  volume_type       = "gp3"

  user_data = templatefile("${get_terragrunt_dir()}/templates/user_data_wrapper.ps1.tpl", {
    compressed_user_data = base64encode(templatefile("${get_terragrunt_dir()}/templates/user_data.ps1.tpl", {
      aws_region     = local.aws_region,
      hostname       = local.hostname,
      admin_password = local.admin_password
    }))
  })

  tags = {
    Environment  = local.env
    Project      = "DreadGOAD"
    Role         = "DomainController"
    Lab          = "${local.deployment_name}-goad"
    Name         = "${local.deployment_name}-dreadgoad-${local.hostname}"
    Domain       = local.domain
    ComputerName = local.hostname
  }
}
