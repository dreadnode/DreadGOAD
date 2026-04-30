locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  deployment_name = local.env_vars.locals.deployment_name
  location        = local.region_vars.locals.location
  vnet_cidr       = local.env_vars.locals.vnet_cidr
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-net"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  deployment_name = local.deployment_name
  env             = local.env
  location        = local.location
  vnet_cidr       = local.vnet_cidr

  additional_tags = {
    Project = "DreadGOAD"
  }
}
