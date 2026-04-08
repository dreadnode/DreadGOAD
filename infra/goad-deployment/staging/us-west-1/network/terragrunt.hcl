locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  aws_region      = local.region_vars.locals.aws_region
  deployment_name = local.env_vars.locals.deployment_name
  vpc_cidr        = local.env_vars.locals.vpc_cidr
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-net"
}

include {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  additional_tags = {
    Project     = "DreadGOAD"
    Environment = local.env
  }
  deployment_name = local.deployment_name
  env             = local.env
  map_public_ip   = true
  vpc_cidr_block  = local.vpc_cidr

  # Security group rules for VPC endpoints
  vpce_security_group_rules = {
    ingress_cidr_blocks = [local.vpc_cidr]
    egress_cidr_blocks  = ["0.0.0.0/0"]
  }

  # VPC endpoints required for SSM-based instance management
  vpc_endpoints = {
    ssm = {
      service     = "ssm"
      type        = "Interface"
      private_dns = true
    }
    ssmmessages = {
      service     = "ssmmessages"
      type        = "Interface"
      private_dns = true
    }
    ec2messages = {
      service     = "ec2messages"
      type        = "Interface"
      private_dns = true
    }
    s3 = {
      service = "s3"
      type    = "Gateway"
    }
  }
}
