# Infrastructure

Terragrunt-based AWS infrastructure for deploying DreadGOAD lab environments.

## Directory Structure

```text
infra/
├── root.hcl                          # Root Terragrunt config (S3 state, AWS provider)
└── goad-deployment/
    ├── host.hcl                      # Host metadata lookup from registry
    ├── host-registry.yaml            # Single source of truth for all GOAD hosts
    └── staging/                      # Environment directory
        ├── env.hcl                   # Account ID, VPC CIDR, deployment name
        └── us-west-1/               # Region directory
            ├── region.hcl           # AWS region setting
            ├── network/             # VPC, subnets, security groups, VPC endpoints
            │   └── terragrunt.hcl
            └── goad/                # Per-host instance configurations
                ├── dc01/terragrunt.hcl   # kingslanding (DC, Win 2019)
                ├── dc02/terragrunt.hcl   # winterfell (DC, Win 2019)
                ├── dc03/terragrunt.hcl   # meereen (DC, Win 2016)
                ├── srv02/terragrunt.hcl  # castelblack (MSSQL, Win 2019)
                └── srv03/terragrunt.hcl  # braavos (Member, Win 2016)
```

## Configuration Hierarchy

Terragrunt merges configuration from multiple levels:

1. **`root.hcl`** -- S3 remote state backend, AWS provider generation, Terraform version constraint (>= 1.7)
2. **`env.hcl`** -- Environment-specific: `deployment_name`, `aws_account_id`, `env`, `vpc_cidr`
3. **`region.hcl`** -- Region-specific: `aws_region`
4. **`host.hcl`** -- Auto-resolves host metadata from `host-registry.yaml` based on directory path
5. **Per-host `terragrunt.hcl`** -- Instance-specific: AMI filters, instance type, passwords, dependencies

## Host Registry

`host-registry.yaml` is the single source of truth for all GOAD hosts. Each entry defines:

- `hostname`, `computer_name`, `goad_id`
- `role` (domain_controller, member_server)
- `os`, `os_version`, `domain`
- `tier`, `groups`, `terragrunt_path`

The `host.hcl` file automatically looks up host metadata based on the current Terragrunt directory path, so host configurations don't need to duplicate registry data.

## Remote State

State is stored in S3 with DynamoDB locking:

- **Bucket**: `dreadgoad-{deployment_name}-{env}-{region}`
- **Key**: `{path_relative_to_include}/terraform.tfstate`
- **Lock table**: `{deployment_name}-tfstate`

Each environment and region gets isolated state.

## Terraform Modules

The Terragrunt configs reference modules from `modules/`:

- **`terraform-aws-net`** -- VPC, subnets, NAT Gateway, VPC endpoints (SSM, Secrets Manager, etc.)
- **`terraform-aws-instance-factory`** -- EC2 instances with SSM management, AMI selection, security groups

## Deployment

```bash
cd infra/goad-deployment/staging/us-west-1

# Deploy networking first
cd network && terragrunt init && terragrunt apply && cd ..

# Deploy all GOAD hosts in parallel
cd goad && terragrunt run-all init && terragrunt run-all apply
```

All instances use SSM for management -- no SSH keys or open ports. VPC endpoints for SSM, SSM Messages, and EC2 Messages are created by the network module.

## Adding a New Environment

```bash
# Duplicate the staging tree
cp -r goad-deployment/staging goad-deployment/dev

# Edit dev/env.hcl with new account ID, VPC CIDR, etc.
```

Each environment gets its own Terraform state, so multiple labs can run in parallel.

## Adding a New Region

```bash
# Create region directory under your environment
mkdir -p goad-deployment/staging/eu-west-1
cp goad-deployment/staging/us-west-1/region.hcl goad-deployment/staging/eu-west-1/
# Edit region.hcl, then copy network/ and goad/ directories
```

For the full end-to-end workflow including warpgate AMI builds and Ansible provisioning, see the [AWS AMI build & deploy workflow](../docs/mkdocs/docs/providers/aws-ami-workflow.md).
