include "host" {
  path   = find_in_parent_folders("host.hcl")
  expose = true
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  host     = include.host.locals.host
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-linux-instance"
}

dependency "network" {
  config_path = "../../network"
  mock_outputs = {
    vpc_id             = "vpc-mock"
    vpc_cidr           = "10.50.0.0/16"
    private_subnet_ids = ["subnet-mock"]
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

inputs = {
  env              = local.env_vars.locals.env
  deployment_name  = local.env_vars.locals.deployment_name
  instance_name    = local.host.host_id
  instance_type    = local.host.instance_type
  vpc_id           = dependency.network.outputs.vpc_id
  vpc_cidr         = dependency.network.outputs.vpc_cidr
  subnet_id        = dependency.network.outputs.private_subnet_ids[0]
  private_ip       = local.host.private_ip
  root_volume_size = local.host.root_volume_size
  data_volume_size = local.host.data_volume_size
  user_data = templatefile("${get_terragrunt_dir()}/../cloud-init.yaml.tpl", {
    hostname = local.host.hostname
  })
  additional_tags = {
    Range      = "GOAT"
    Deployment = "goat-deployment"
    Role       = local.host.role
  }
}
