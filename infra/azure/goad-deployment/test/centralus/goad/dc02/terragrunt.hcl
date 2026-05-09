# =============================================================================
# DC02 - GOAD lab host (see host-registry.yaml)
# Marketplace image: Windows Server 2019 Datacenter
# =============================================================================

include "host" {
  path   = "${get_repo_root()}/infra/goad-deployment/host.hcl"
  expose = true
}

locals {
  env_vars    = read_terragrunt_config(find_in_parent_folders("env.hcl"))
  region_vars = read_terragrunt_config(find_in_parent_folders("region.hcl"))

  env             = local.env_vars.locals.env
  deployment_name = local.env_vars.locals.deployment_name
  location        = local.region_vars.locals.location

  hostname = include.host.locals.computer_name
  goad_id  = include.host.locals.goad_id

  lab_config     = jsondecode(file("${get_repo_root()}/ad/GOAD/data/${local.env}-config.json"))
  admin_password = local.lab_config.lab.hosts[local.goad_id].local_admin_password
}

terraform {
  source = "${get_repo_root()}/modules//terraform-azure-instance-factory"
}

dependency "network" {
  config_path = "../../network"
  mock_outputs = {
    resource_group_name = "mock-rg"
    location            = "centralus"
    private_subnet_id   = "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/mock-rg/providers/Microsoft.Network/virtualNetworks/mock-vnet/subnets/mock-subnet"
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env           = local.env
  instance_name = "${local.deployment_name}-dreadgoad-${local.hostname}"
  computer_name = local.hostname
  instance_size = "Standard_D2s_v3"
  source_image = {
    publisher = "MicrosoftWindowsServer"
    offer     = "WindowsServer"
    sku       = "2019-Datacenter"
    version   = "latest"
  }
  resource_group_name = dependency.network.outputs.resource_group_name
  location            = dependency.network.outputs.location
  subnet_id           = dependency.network.outputs.private_subnet_id
  admin_password      = local.admin_password

  bootstrap_script = templatefile(
    "${get_repo_root()}/infra/azure/goad-deployment/test/centralus/goad/templates/bootstrap.ps1.tpl",
    {
      admin_password = local.admin_password
    },
  )

  tags = {
    Project      = "DreadGOAD"
    Role         = "DomainController"
    Lab          = "${local.deployment_name}-goad"
    Domain       = include.host.locals.domain
    ComputerName = local.hostname
    GoadId       = local.goad_id
  }
}
