locals {
  deployment_name = "scope-range"
  env             = "scope-dev"

  vnet_cidr            = "10.50.0.0/16"
  workload_subnet_cidr = "10.50.10.0/24"
  bastion_subnet_cidr  = "10.50.20.0/26"
  range_dns_server     = "10.50.10.60"
  admin_username       = "scopeadmin"
  operator_key_path    = pathexpand("~/.dreadgoad/keys/azure-scope-dev-scope-range-admin")
  kali_instance_size   = "Standard_D4s_v3"
}
