# =============================================================================
# Optional Kali Linux Attack Box
#
# Deploys a headless Kali VM in a private subnet. Access is through AWS Systems
# Manager Session Manager; no public IP or inbound SSH rule is created.
# Enable with `dreadgoad infra apply --with-kali`.
# =============================================================================

exclude {
  if      = lower(get_env("DREADGOAD_ENABLE_AWS_KALI", "false")) != "true"
  actions = ["all"]
}

locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))

  env                = local.env_vars.locals.env
  deployment_name    = local.env_vars.locals.deployment_name
  kali_instance_type = try(local.env_vars.locals.kali_instance_type, "t3.medium")
}

terraform {
  source = "${get_repo_root()}/modules//terraform-aws-kali"
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    vpc_id             = "vpc-mock"
    vpc_cidr           = "10.0.0.0/16"
    private_subnet_ids = ["subnet-mock"]
  }
  mock_outputs_allowed_terraform_commands = ["init", "validate", "plan"]
}

include {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  env             = local.env
  deployment_name = local.deployment_name
  instance_type   = local.kali_instance_type
  vpc_id          = dependency.network.outputs.vpc_id
  vpc_cidr        = dependency.network.outputs.vpc_cidr
  subnet_id       = dependency.network.outputs.private_subnet_ids[0]

  additional_tags = {
    Project = "DreadGOAD"
    Lab     = "${local.deployment_name}-goad"
  }
}
