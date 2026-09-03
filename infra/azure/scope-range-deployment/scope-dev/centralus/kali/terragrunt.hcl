locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-kali"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    resource_group_name = "scope-dev-scope-range-rg"
    location            = "centralus"
    vnet_name           = "scope-dev-scope-range-vnet"
    vnet_cidr           = "10.50.0.0/16"
    private_subnet_id   = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/scope-dev-scope-range-rg/providers/Microsoft.Network/virtualNetworks/scope-dev-scope-range-vnet/subnets/scope-dev-scope-range-private"
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

dependency "access" {
  config_path = "../access"
  mock_outputs = {
    public_key_openssh = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGFiHzcUOlS2dr9JBEmZ97cCtKMCr0gSpYEMfwukjeoc scope-plan-mock"
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env                  = local.env_vars.locals.env
  deployment_name      = local.env_vars.locals.deployment_name
  instance_name        = "kali01"
  computer_name        = "kali01"
  location             = dependency.network.outputs.location
  resource_group_name  = dependency.network.outputs.resource_group_name
  virtual_network_name = dependency.network.outputs.vnet_name
  subnet_id            = dependency.network.outputs.private_subnet_id
  private_ip_address   = "10.50.10.10"
  instance_size        = local.env_vars.locals.kali_instance_size
  os_disk_size_gb      = 100
  admin_ssh_public_key = dependency.access.outputs.public_key_openssh

  additional_tags = {
    Project    = "DreadGOAD"
    Lab        = "SCOPE-RANGE"
    Deployment = "scope-range-deployment"
  }
}
