# Set common variables for the environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "dreadgoad" # Change to your deployment name
  aws_account_id  = get_aws_account_id()
  env             = "staging"     # Environment name (dev, staging, prod)
  vpc_cidr        = "10.1.0.0/16" # VPC CIDR block for this environment

  # Optional Kali attack box. Enable with --with-kali on infra commands.
  kali_instance_type = "t3.medium"
}
