# Terraform Modules

Reusable Terraform modules used by the DreadGOAD Terragrunt configurations.

## Modules

### [terraform-aws-instance-factory](terraform-aws-instance-factory/)

Flexible EC2 instance provisioning supporting Linux, Windows, and macOS. Used by each GOAD host's `terragrunt.hcl` to deploy domain controllers and member servers.

Key features:

- AMI selection with configurable filters (used to select pre-baked warpgate AMIs)
- SSM-based management (no SSH keys or open ports)
- IAM instance profiles with SSM permissions
- Encrypted EBS volumes
- Security group management
- Optional ASG, ALB, and NLB support

### [terraform-aws-net](terraform-aws-net/)

AWS VPC and networking infrastructure. Used by the `network/terragrunt.hcl` to create the lab network.

Key features:

- VPC with public and private subnets across multiple AZs
- NAT Gateway for outbound internet access
- VPC endpoints for AWS services (SSM, Secrets Manager, ECR, CloudWatch, S3)
- Security groups for VPC endpoint access
- Terragrunt-compatible structure

## How They're Used

The Terragrunt configurations under `infra/goad-deployment/` reference these modules:

```text
infra/goad-deployment/{env}/{region}/
├── network/terragrunt.hcl    → uses terraform-aws-net
└── goad/
    ├── dc01/terragrunt.hcl   → uses terraform-aws-instance-factory
    ├── dc02/terragrunt.hcl   → uses terraform-aws-instance-factory
    ├── dc03/terragrunt.hcl   → uses terraform-aws-instance-factory
    ├── srv02/terragrunt.hcl  → uses terraform-aws-instance-factory
    └── srv03/terragrunt.hcl  → uses terraform-aws-instance-factory
```

Each module has its own detailed README with full input/output documentation.
