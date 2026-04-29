terraform {
  source = "${get_repo_root()}/modules//terraform-azure-vm"
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

inputs = {
  vm_name  = "dreadgoad-azure"
  location = "eastus"

  tags = {
    Project = "DreadGOAD"
    Purpose = "azure-auth-validation"
  }
}
