# Set common variables for the test environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "dreadgoad"
  aws_account_id  = get_aws_account_id()
  env             = "test"
  vpc_cidr        = "10.8.0.0/16"

  # Optional Kali attack box. Enable with --with-kali on infra commands.
  kali_instance_type = "t3.medium"
}
