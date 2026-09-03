locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-net"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  deployment_name     = local.env_vars.locals.deployment_name
  env                 = local.env_vars.locals.env
  location            = local.region_vars.locals.location
  vnet_cidr           = local.env_vars.locals.vnet_cidr
  private_subnet_cidr = local.env_vars.locals.workload_subnet_cidr

  additional_tags = {
    Project    = "DreadGOAD"
    Lab        = "SCOPE-RANGE"
    Deployment = "scope-range-deployment"
  }
}
