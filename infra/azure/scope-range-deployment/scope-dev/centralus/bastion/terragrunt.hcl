locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-bastion"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    resource_group_name = "scope-dev-scope-range-rg"
    location            = "centralus"
    vnet_id             = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/scope-dev-scope-range-rg/providers/Microsoft.Network/virtualNetworks/scope-dev-scope-range-vnet"
    vnet_name           = "scope-dev-scope-range-vnet"
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env                  = local.env_vars.locals.env
  deployment_name      = local.env_vars.locals.deployment_name
  location             = dependency.network.outputs.location
  resource_group_name  = dependency.network.outputs.resource_group_name
  virtual_network_id   = dependency.network.outputs.vnet_id
  virtual_network_name = dependency.network.outputs.vnet_name
  sku                  = "Standard"
  bastion_subnet_cidr  = local.env_vars.locals.bastion_subnet_cidr
  tunneling_enabled    = true

  additional_tags = {
    Project    = "DreadGOAD"
    Lab        = "SCOPE-RANGE"
    Deployment = "scope-range-deployment"
    Role       = "Bastion"
  }
}
