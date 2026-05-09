# =============================================================================
# Azure Bastion (optional)
# Enable by setting DREADGOAD_ENABLE_AZURE_BASTION=true before Terragrunt runs.
# =============================================================================

exclude {
  if      = lower(get_env("DREADGOAD_ENABLE_AZURE_BASTION", "false")) != "true"
  actions = ["all"]
}

locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env                       = local.env_vars.locals.env
  deployment_name           = local.env_vars.locals.deployment_name
  location                  = local.region_vars.locals.location
  bastion_sku               = local.env_vars.locals.bastion_sku
  bastion_subnet_cidr       = local.env_vars.locals.bastion_subnet_cidr
  bastion_tunneling_enabled = local.env_vars.locals.bastion_tunneling_enabled
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-bastion"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    resource_group_name = "mock-rg"
    location            = "centralus"
    vnet_id             = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mock-rg/providers/Microsoft.Network/virtualNetworks/mock-vnet"
    vnet_name           = "mock-vnet"
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env                  = local.env
  deployment_name      = local.deployment_name
  location             = dependency.network.outputs.location
  resource_group_name  = dependency.network.outputs.resource_group_name
  virtual_network_id   = dependency.network.outputs.vnet_id
  virtual_network_name = dependency.network.outputs.vnet_name

  sku                 = local.bastion_sku
  bastion_subnet_cidr = local.bastion_subnet_cidr
  tunneling_enabled   = local.bastion_tunneling_enabled

  additional_tags = {
    Project = "DreadGOAD"
    Role    = "Bastion"
    Lab     = "${local.deployment_name}-goad"
  }
}
