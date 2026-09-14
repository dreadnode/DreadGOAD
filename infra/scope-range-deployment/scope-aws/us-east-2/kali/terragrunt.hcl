locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-kali"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    vpc_id             = "vpc-mock"
    vpc_cidr           = "10.50.0.0/16"
    private_subnet_ids = ["subnet-mock"]
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env              = local.env_vars.locals.env
  deployment_name  = local.env_vars.locals.deployment_name
  instance_name    = "kali01"
  lab_name         = "GOAT"
  instance_type    = local.env_vars.locals.kali_instance_type
  root_volume_size = 100
  vpc_id           = dependency.network.outputs.vpc_id
  vpc_cidr         = dependency.network.outputs.vpc_cidr
  subnet_id        = dependency.network.outputs.private_subnet_ids[0]
  private_ip       = "10.50.10.10"

  additional_tags = {
    Project    = "DreadGOAD"
    Range      = "GOAT"
    Deployment = "scope-range-deployment"
  }
}
