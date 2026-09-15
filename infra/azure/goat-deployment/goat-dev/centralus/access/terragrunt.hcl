locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))
}

terraform {
  source = "${get_repo_root()}/modules//terraform-local-ssh-key"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  private_key_path = local.env_vars.locals.operator_key_path
}
