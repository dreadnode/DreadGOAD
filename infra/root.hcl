# ---------------------------------------------------------------------------------------------------------------------
# TERRAGRUNT CONFIGURATION
# Root configuration for DreadGOAD infrastructure deployments.
# Configures S3 remote state, AWS provider, and shared variables.
# ---------------------------------------------------------------------------------------------------------------------
locals {
  env_vars = read_terragrunt_config(find_in_parent_folders("env.hcl"))

  aws_region = coalesce(
    get_env("AWS_DEFAULT_REGION", ""),
    can(read_terragrunt_config(find_in_parent_folders("region.hcl"))) ? read_terragrunt_config(find_in_parent_folders("region.hcl")).locals.aws_region : "us-east-1"
  )

  deployment_name = local.env_vars.locals.deployment_name
  account_id      = local.env_vars.locals.aws_account_id
  env             = local.env_vars.locals.env
}

generate "versions" {
  path      = "versions_override.tf"
  if_exists = "overwrite_terragrunt"
  contents  = <<EOF
terraform {
  required_version = ">= 1.7"
}
EOF
}

generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite_terragrunt"
  contents  = <<EOF
provider "aws" {
  region              = "${local.aws_region}"
  allowed_account_ids = ["${local.account_id}"]
}
EOF
}

remote_state {
  backend = "s3"
  config = {
    encrypt        = true
    bucket         = join("-", ["dreadgoad", local.deployment_name, local.env, local.aws_region])
    key            = "${path_relative_to_include()}/terraform.tfstate"
    region         = local.aws_region
    dynamodb_table = join("-", [local.deployment_name, "tfstate"])
  }
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
}

inputs = merge(
  local.env_vars.locals,
)
