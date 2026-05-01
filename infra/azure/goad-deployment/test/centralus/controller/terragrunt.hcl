# =============================================================================
# In-VNet Ansible Controller (optional)
#
# Why this exists: Azure has no first-class equivalent of Ansible-over-SSM.
# Microsoft's recommended pattern for "no public exposure" is to tunnel each
# host through Bastion, which doesn't fan out cleanly. An in-VNet controller
# keeps the targets private, gives Ansible normal SSH/WinRM/PSRP, and only
# requires one human-facing tunnel: Bastion -> controller.
#
# Enable by setting DREADGOAD_ENABLE_AZURE_CONTROLLER=true before Terragrunt
# runs. Requires Bastion to be reachable (set DREADGOAD_ENABLE_AZURE_BASTION
# alongside, otherwise you can't get in).
# =============================================================================

exclude {
  if      = lower(get_env("DREADGOAD_ENABLE_AZURE_CONTROLLER", "false")) != "true"
  actions = ["all"]
}

locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  deployment_name = local.env_vars.locals.deployment_name
  location        = local.region_vars.locals.location

  controller_subnet_cidr               = local.env_vars.locals.controller_subnet_cidr
  controller_ssh_source_address_prefix = local.env_vars.locals.controller_ssh_source_address_prefix
  controller_instance_size             = local.env_vars.locals.controller_instance_size

  # SSH key resolution order:
  #   1. DREADGOAD_AZURE_CONTROLLER_SSH_KEY      - inline public-key contents
  #   2. DREADGOAD_AZURE_CONTROLLER_SSH_KEY_PATH - explicit path to a pubkey
  #   3. fall through to module-generated ephemeral keypair
  #
  # When (1) and (2) are both unset, the module creates a fresh ed25519 key
  # and writes the privkey to ephemeral_key_path. No personal key gets
  # baked into the lab.
  ssh_key_inline    = get_env("DREADGOAD_AZURE_CONTROLLER_SSH_KEY", "")
  ssh_key_path_var  = get_env("DREADGOAD_AZURE_CONTROLLER_SSH_KEY_PATH", "")
  ssh_key_from_path = local.ssh_key_path_var != "" && fileexists(local.ssh_key_path_var) ? trimspace(file(local.ssh_key_path_var)) : ""
  admin_ssh_public_key = (
    local.ssh_key_inline != "" ? local.ssh_key_inline :
    local.ssh_key_from_path != "" ? local.ssh_key_from_path :
    null
  )

  ephemeral_key_path = pathexpand("~/.dreadgoad/keys/azure-${local.env}-${local.deployment_name}-controller")
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-controller"
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

  controller_subnet_cidr    = local.controller_subnet_cidr
  ssh_source_address_prefix = local.controller_ssh_source_address_prefix
  instance_size             = local.controller_instance_size

  admin_ssh_public_key      = local.admin_ssh_public_key
  ephemeral_key_output_path = local.ephemeral_key_path

  additional_tags = {
    Project = "DreadGOAD"
    Lab     = "${local.deployment_name}-goad"
  }
}
