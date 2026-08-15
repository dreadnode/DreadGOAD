# =============================================================================
# Optional Kali Linux Attack Box
#
# Deploys a headless Kali VM on its own subnet for attacking the lab from
# inside the VNet. Access is via Azure Bastion SSH — no public IP.
#
# Enable by setting DREADGOAD_ENABLE_AZURE_KALI=true before Terragrunt runs,
# or use `dreadgoad infra apply --with-kali`. Requires marketplace terms to be
# accepted: az vm image terms accept --publisher kali-linux --offer kali --plan kali-2026-2
# =============================================================================

exclude {
  if      = lower(get_env("DREADGOAD_ENABLE_AZURE_KALI", "false")) != "true"
  actions = ["all"]
}

locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  deployment_name = local.env_vars.locals.deployment_name
  location        = local.region_vars.locals.location

  kali_subnet_cidr               = local.env_vars.locals.kali_subnet_cidr
  kali_ssh_source_address_prefix = local.env_vars.locals.kali_ssh_source_address_prefix
  kali_instance_size             = local.env_vars.locals.kali_instance_size

  # SSH key resolution — same 3-tier pattern as the controller module.
  ssh_key_inline    = get_env("DREADGOAD_AZURE_KALI_SSH_KEY", "")
  ssh_key_path_var  = get_env("DREADGOAD_AZURE_KALI_SSH_KEY_PATH", "")
  ssh_key_from_path = local.ssh_key_path_var != "" && fileexists(local.ssh_key_path_var) ? trimspace(file(local.ssh_key_path_var)) : ""
  admin_ssh_public_key = (
    local.ssh_key_inline != "" ? local.ssh_key_inline :
    local.ssh_key_from_path != "" ? local.ssh_key_from_path :
    null
  )

  ephemeral_key_path = pathexpand("~/.dreadgoad/keys/azure-${local.env}-${local.deployment_name}-kali")
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-kali"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    resource_group_name = "mock-rg"
    location            = "centralus"
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
  virtual_network_name = dependency.network.outputs.vnet_name

  kali_subnet_cidr          = local.kali_subnet_cidr
  ssh_source_address_prefix = local.kali_ssh_source_address_prefix
  instance_size             = local.kali_instance_size

  admin_ssh_public_key      = local.admin_ssh_public_key
  ephemeral_key_output_path = local.ephemeral_key_path

  additional_tags = {
    Project = "DreadGOAD"
    Lab     = "${local.deployment_name}"
  }
}
