# NOTE: AMI IDs below are region-specific (originally eu-west-1).
# Replace with AMI IDs for your target AWS region.
# See: https://aws.amazon.com/marketplace for Windows Server base AMIs

"dc01" = {
  name               = "dc01"
  domain             = "sevenkingdoms.local"
  windows_sku        = "2019-Datacenter"
  ami                = "ami-03440f0d88fea1060"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.10"
  password           = "8dCT-DJjgScp"
}
