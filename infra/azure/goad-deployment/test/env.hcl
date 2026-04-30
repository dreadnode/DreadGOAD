locals {
  deployment_name = "goad"
  env             = "test"
  vnet_cidr       = "10.8.0.0/16"

  bastion_sku               = "Standard"
  bastion_subnet_cidr       = "10.8.2.0/26"
  bastion_tunneling_enabled = true

  # In-VNet Ansible controller. Reached via Bastion SSH; no public IP.
  # ssh_source_address_prefix is intentionally the bastion subnet CIDR so
  # only Bastion-brokered traffic can hit port 22.
  controller_subnet_cidr               = "10.8.3.0/28"
  controller_ssh_source_address_prefix = "10.8.2.0/26"
  # Standard_B2s is the natural cheap default but is capacity-restricted in
  # centralus for the Dreadnode MSFT Startup subscription. D2s_v3 is what
  # the GOAD DCs run on, so capacity is proven. Cost trade is small for a
  # transient lab.
  controller_instance_size = "Standard_D2s_v3"
}
