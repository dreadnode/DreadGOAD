include "host" {
  path   = find_in_parent_folders("host.hcl")
  expose = true
}
include "root" { path = find_in_parent_folders("root.hcl") }
locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  host     = include.host.locals.host
}
terraform { source = "${get_repo_root()}/modules//terraform-azure-linux-instance" }
dependency "network" {
  config_path                             = "../../network"
  mock_outputs                            = { resource_group_name = "scope-dev-scope-range-rg", location = "centralus", private_subnet_id = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/scope-dev-scope-range-rg/providers/Microsoft.Network/virtualNetworks/scope-dev-scope-range-vnet/subnets/scope-dev-scope-range-private" }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}
dependency "access" {
  config_path                             = "../../access"
  mock_outputs                            = { public_key_openssh = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGFiHzcUOlS2dr9JBEmZ97cCtKMCr0gSpYEMfwukjeoc scope-plan-mock" }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}
inputs = {
  env                  = local.env_vars.locals.env
  deployment_name      = local.env_vars.locals.deployment_name
  instance_name        = local.host.host_id
  computer_name        = local.host.hostname
  location             = dependency.network.outputs.location
  resource_group_name  = dependency.network.outputs.resource_group_name
  subnet_id            = dependency.network.outputs.private_subnet_id
  private_ip_address   = local.host.private_ip
  instance_size        = local.host.instance_size
  admin_username       = local.env_vars.locals.admin_username
  admin_ssh_public_key = dependency.access.outputs.public_key_openssh
  os_disk_size_gb      = local.host.os_disk_size_gb
  data_disks           = { data = { lun = 0, size_gb = local.host.data_disk_size_gb } }
  custom_data          = templatefile("${get_terragrunt_dir()}/../cloud-init.yaml.tpl", { hostname = local.host.hostname })
  tags                 = { Project = "DreadGOAD", Lab = "SCOPE-RANGE", Deployment = "scope-range-deployment", Role = local.host.role }
}
