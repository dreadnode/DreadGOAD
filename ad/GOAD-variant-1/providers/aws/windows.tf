# t2.medium = 2cpu / 4GB
# t2.large  = 2cpu / 8GB
# t2.xlarge = 4cpu / 16GB

# NOTE: AMI IDs below are region-specific (originally eu-west-1).
# Replace with AMI IDs for your target AWS region.
# See: https://aws.amazon.com/marketplace for Windows Server base AMIs

"dc01" = {
  name               = "dc01"
  domain             = "deltasystems.local"
  windows_sku        = "2019-Datacenter"
  ami                = "ami-07b70ee6215b30557"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.10"
  password           = "8dCT-DJjgScp"
}
"dc02" = {
  name               = "dc02"
  domain             = "hq.deltasystems.local"
  windows_sku        = "2019-Datacenter"
  ami                = "ami-07b70ee6215b30557"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.11"
  password           = "NgtI75cKV+Pu"
}
"dc03" = {
  name               = "dc03"
  domain             = "vortexindustries.local"
  windows_sku        = "2016-Datacenter"
  ami                = "ami-0dbedda2d0007250e"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.12"
  password           = "Ufe-bVXSx9rk"
}
"srv02" = {
  name               = "srv02"
  domain             = "hq.deltasystems.local"
  windows_sku        = "2019-Datacenter"
  ami                = "ami-07b70ee6215b30557"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.22"
  password           = "NgtI75cKV+Pu"
}
"srv03" = {
  name               = "srv03"
  domain             = "vortexindustries.local"
  windows_sku        = "2016-Datacenter"
  ami                = "ami-07b70ee6215b30557"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.23"
  password           = "978i2pF43UJ-"
}
