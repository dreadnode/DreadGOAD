# Set common variables for the test environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "goad"
  aws_account_id  = get_aws_account_id()
  env             = "test"
  vpc_cidr        = "10.8.0.0/16"
}
