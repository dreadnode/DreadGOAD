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

# Keep local state outside .terragrunt-cache so clearing the cache cannot lose
# ownership of live Azure resources. The state directory is gitignored.
remote_state {
  backend = "local"
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite_terragrunt"
  }
  config = {
    path = "${get_repo_root()}/.dreadgoad/state/azure/scope-range/${path_relative_to_include()}/terraform.tfstate"
  }
}
