# Set common variables for the environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "goad"           # Change to your deployment name
  aws_account_id  = "CHANGE_ME"      # Your AWS account ID
  env             = "staging"        # Environment name (dev, staging, prod)
  vpc_cidr        = "10.1.0.0/16"   # VPC CIDR block for this environment
}
