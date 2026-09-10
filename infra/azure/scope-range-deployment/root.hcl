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
provider "azurerm" {
  features {}
}
EOF
}

# Keep local state outside both .terragrunt-cache and the repository so clearing
# caches or replacing a checkout cannot lose ownership of live Azure resources.
locals {
  scope_state_root = pathexpand("~/.dreadgoad/state/azure/scope-range")
}

remote_state {
  backend = "local"
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
  config = {
    path = "${local.scope_state_root}/${path_relative_to_include()}/terraform.tfstate"
  }
}
