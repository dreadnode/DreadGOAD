# NOTE: AMI IDs below are region-specific (originally eu-west-1).
# Replace with AMI IDs for your target AWS region.
# See: https://aws.amazon.com/marketplace for Windows Server base AMIs

"lx01" = {
  name               = "lx01"
  linux_sku          = "24_04-lts-gen2"
  linux_version      = "latest"
  ami                = "ami-0bb8b77ad97138af1"
  private_ip_address = "{{ip_range}}.12"
  password           = "HGLXaxQSP@ssw_rd$"
  instance_type      = "t3.medium"
}
