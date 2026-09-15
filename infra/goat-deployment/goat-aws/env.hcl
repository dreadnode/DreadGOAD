locals {
  deployment_name = "goat"
  env             = "goat-aws"
  aws_account_id  = get_aws_account_id()
  state_env_slug  = trim(regexreplace(lower(local.env), "[^a-z0-9-]", "-"), "-")
  state_bucket_prefix = join("-", [
    "dreadgoad",
    "goat",
    local.aws_account_id,
    substr(local.state_env_slug, 0, 12),
    substr(sha256(local.env), 0, 8),
  ])

  vpc_cidr             = "10.50.0.0/16"
  public_subnet_cidrs  = ["10.50.0.0/24"]
  private_subnet_cidrs = ["10.50.10.0/24"]
  kali_instance_type   = "m6i.xlarge"
}
