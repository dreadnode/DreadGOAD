locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-net"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env                  = local.env_vars.locals.env
  deployment_name      = local.env_vars.locals.deployment_name
  vpc_cidr_block       = local.env_vars.locals.vpc_cidr
  public_subnet_cidrs  = local.env_vars.locals.public_subnet_cidrs
  private_subnet_cidrs = local.env_vars.locals.private_subnet_cidrs
  map_public_ip        = true

  vpce_security_group_rules = {
    ingress_cidr_blocks = [local.env_vars.locals.vpc_cidr]
    egress_cidr_blocks  = ["0.0.0.0/0"]
  }

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

  additional_tags = {
    Project    = "DreadGOAD"
    Lab        = "SCOPE-RANGE"
    Deployment = "scope-range-deployment"
  }
}
