# AWS AMI Build & Deploy Workflow

This guide covers the end-to-end workflow for deploying DreadGOAD on AWS: building pre-baked AMIs with warpgate, configuring Terragrunt, deploying infrastructure, and provisioning the lab with Ansible.

## Overview

```text
warpgate build (golden AMIs)
        |
        v
terragrunt apply (AWS infrastructure)
        |
        v
ansible provisioning (AD configuration)
```

Building pre-baked AMIs saves approximately **170 minutes** per deployment by pre-installing Windows Updates, AD DS roles, MSSQL, and other dependencies that would otherwise install at runtime.

## Prerequisites

- [warpgate](https://github.com/dreadnode/warpgate) CLI installed
- [Terraform](https://www.terraform.io/downloads.html) >= 1.7
- [Terragrunt](https://terragrunt.gruntwork.io/) installed
- [AWS CLI](https://aws.amazon.com/cli/) configured with appropriate credentials
- [Ansible](https://docs.ansible.com/) >= 2.15
- Go 1.21+ (for the `dreadgoad` CLI)

## Environment and Region

The `--env` and `--region` flags thread through the entire stack -- they determine which Terragrunt directory tree is used and which Ansible inventory the CLI targets. Understanding this mapping is important before you start.

### How env and region map to infrastructure

The `dreadgoad` CLI uses `--env` and `--region` to locate your Terragrunt configuration and Ansible inventory:

```text
infra/goad-deployment/{env}/{region}/
                       │       │
                       │       └── region.hcl + network/ + goad/{dc01,dc02,...}
                       └── env.hcl (account ID, VPC CIDR, deployment name)
```

For example, `--env staging --region us-west-1` maps to `infra/goad-deployment/staging/us-west-1/`. The Ansible inventory is resolved as `{env}-inventory` (e.g., `staging-inventory`).

!!! warning "Keep these consistent"
    The `--env` and `--region` you pass to `dreadgoad` CLI commands must match the Terragrunt directory structure you deployed into. If you ran `terragrunt apply` under `staging/us-west-1/`, then use `--env staging --region us-west-1` for provisioning and health checks.

### Setting env and region

You have three options (highest priority wins):

| Method | Example | Notes |
|--------|---------|-------|
| CLI flags | `dreadgoad provision --env staging --region us-west-1` | Highest priority, overrides everything |
| Environment variables | `export DREADGOAD_ENV=staging` | Useful for CI or shell sessions |
| Config file | `dreadgoad config set env staging` | Persistent defaults at `~/.config/dreadgoad/dreadgoad.yaml` |

The config file is **optional** -- the CLI works with just flags or environment variables. If nothing is set, the defaults are `env=staging` and `region` is resolved from your Ansible inventory.

To initialize a config file with defaults:

```bash
dreadgoad config init    # Creates ~/.config/dreadgoad/dreadgoad.yaml
dreadgoad config show    # View the effective configuration
```

For full details on all config options, see [CLI configuration](../../cli.md).

### Choosing an environment

The repo ships with a `staging` directory tree. To use a different environment (e.g., `dev`), duplicate the directory structure:

```bash
cp -r infra/goad-deployment/staging infra/goad-deployment/dev
```

Then edit `dev/env.hcl` to set `env = "dev"` and adjust the account ID, VPC CIDR, or other settings as needed. Each environment gets its own Terraform state, so you can run multiple labs in parallel.

Throughout this guide, examples use `staging` and `us-west-1` to match the defaults. Replace with your chosen env and region as needed.

## Step 1: Build Golden AMIs with Warpgate

DreadGOAD provides three warpgate templates under `warpgate-templates/`:

| Template | Target Hosts | OS | Saves |
|----------|-------------|-----|-------|
| `goad-dc-base` | DC01, DC02 | Windows Server 2019 | ~25 min/host |
| `goad-dc-base-2016` | DC03 | Windows Server 2016 | ~25 min/host |
| `goad-mssql-base` | SRV02 | Windows Server 2019 | ~48 min/host |

### Build all AMIs

```bash
# Domain Controllers (Windows 2019)
warpgate build goad-dc-base --target ami

# Domain Controller (Windows 2016, for DC03/meereen)
warpgate build goad-dc-base-2016 --target ami

# Member Server with MSSQL (Windows 2019, for SRV02/castelblack)
warpgate build goad-mssql-base --target ami
```

To build for a specific region:

```bash
warpgate build goad-dc-base --target ami --vars aws_region=us-west-1
```

### Record the AMI IDs

Each build outputs an AMI ID (e.g., `ami-0abc1234def56789`). Record these -- you'll need them in the next step:

| Template | AMI ID | Used By |
|----------|--------|---------|
| `goad-dc-base` | `ami-xxxxxxxxx` | DC01 (kingslanding), DC02 (winterfell) |
| `goad-dc-base-2016` | `ami-xxxxxxxxx` | DC03 (meereen) |
| `goad-mssql-base` | `ami-xxxxxxxxx` | SRV02 (castelblack) |

!!! note "SRV03 (braavos)"
    SRV03 runs Windows Server 2016 as a member server. If you don't have a dedicated `goad-member-base-2016` AMI, you can use `goad-dc-base-2016` (the extra AD DS role won't interfere) or a vanilla Windows Server 2016 AMI.

### What's pre-baked vs. runtime

Pre-baked AMIs install roles and software but do **not** perform domain-specific configuration. This split keeps AMIs reusable across deployments:

| Pre-baked (in AMI) | Runtime (Ansible) |
|---|---|
| Windows Updates | AD domain promotion |
| AD DS role (unpromoted) | User/group creation |
| DNS, RSAT tools | Trust relationships |
| MSSQL Express (mssql-base) | GPO configuration |
| IIS/WebDAV (mssql-base) | LAPS, ADCS |
| PowerShell DSC modules | Vulnerability injection |
| SSM agent configuration | Domain joins |

## Step 2: Configure Terragrunt

### Set your AWS account

Edit `infra/goad-deployment/staging/env.hcl`:

```hcl
locals {
  deployment_name = "goad"
  aws_account_id  = "123456789012"  # Your AWS account ID
  env             = "staging"
  vpc_cidr        = "10.1.0.0/16"
}
```

### Set your region

Edit `infra/goad-deployment/staging/us-west-1/region.hcl` (or create a new region directory):

```hcl
locals {
  aws_region = "us-west-1"
}
```

### Insert AMI IDs into host configurations

Each host has a `terragrunt.hcl` under `infra/goad-deployment/staging/us-west-1/goad/`. Update the `additional_windows_ami_filters` block in each:

**DC01 and DC02** (`dc01/terragrunt.hcl`, `dc02/terragrunt.hcl`) -- use `goad-dc-base` AMI:

```hcl
additional_windows_ami_filters = [
  {
    name   = "image-id"
    values = ["ami-xxxxxxxxx"]  # goad-dc-base AMI ID
  }
]

windows_os         = "Windows_Server"
windows_os_version = "2019-English-Full-Base"
windows_ami_owners = ["self"]
```

**DC03** (`dc03/terragrunt.hcl`) -- use `goad-dc-base-2016` AMI:

```hcl
additional_windows_ami_filters = [
  {
    name   = "image-id"
    values = ["ami-xxxxxxxxx"]  # goad-dc-base-2016 AMI ID
  }
]

windows_os         = "Windows_Server"
windows_os_version = "2016-English-Full-Base"
windows_ami_owners = ["self"]
```

**SRV02** (`srv02/terragrunt.hcl`) -- use `goad-mssql-base` AMI:

```hcl
additional_windows_ami_filters = [
  {
    name   = "image-id"
    values = ["ami-xxxxxxxxx"]  # goad-mssql-base AMI ID
  }
]

windows_os         = "Windows_Server"
windows_os_version = "2019-English-Full-Base"
windows_ami_owners = ["self"]
```

### Set admin passwords

Set per-host passwords via environment variables:

```bash
export TF_VAR_goad_dc01_password="YourSecurePassword1"
export TF_VAR_goad_dc02_password="YourSecurePassword2"
export TF_VAR_goad_dc03_password="YourSecurePassword3"
export TF_VAR_goad_srv02_password="YourSecurePassword4"
export TF_VAR_goad_srv03_password="YourSecurePassword5"
```

## Step 3: Deploy Infrastructure with Terragrunt

### Initialize and apply

```bash
cd infra/goad-deployment/staging/us-west-1

# Deploy networking first
cd network
terragrunt init
terragrunt apply
cd ..

# Deploy all GOAD hosts
cd goad
terragrunt run-all init
terragrunt run-all apply
```

!!! tip
    `terragrunt run-all` deploys DC01-DC03, SRV02, and SRV03 in parallel. The dependency on the network module is resolved automatically.

### Verify instances

All instances use SSM for management -- no SSH keys or open ports required:

```bash
# Check instance status via AWS CLI
aws ec2 describe-instances \
  --filters "Name=tag:Project,Values=DreadGOAD" \
  --query "Reservations[].Instances[].[Tags[?Key=='Name'].Value|[0],State.Name,InstanceId]" \
  --output table

# Connect to an instance via SSM
aws ssm start-session --target <instance-id>
```

Or use the DreadGOAD CLI:

```bash
dreadgoad health-check --env staging --region us-west-1
```

## Step 4: Provision with Ansible

Once all instances are running, provision the Active Directory environment:

```bash
# Full provisioning (env and region from config defaults or flags)
dreadgoad provision --env staging --region us-west-1

# Resume from a specific playbook (useful after a failure)
dreadgoad provision --env staging --region us-west-1 --from ad-data.yml

# Run only specific playbooks
dreadgoad provision --env staging --plays build.yml,ad-servers.yml

# Limit to specific hosts
dreadgoad provision --env staging --plays ad-data.yml --limit dc01
```

!!! tip
    If you set defaults via config file (`dreadgoad config set env staging`), you can omit the flags: `dreadgoad provision`

Or run Ansible directly for more control:

```bash
cd ansible
ansible-playbook -i ../ad/GOAD/data/inventory -i ../ad/GOAD/providers/aws/inventory main.yml
```

For step-by-step provisioning (useful for debugging):

```bash
ANSIBLE_CMD="ansible-playbook -i ../ad/GOAD/data/inventory -i ../ad/GOAD/providers/aws/inventory"
$ANSIBLE_CMD build.yml            # Prerequisites and VM prep
$ANSIBLE_CMD ad-servers.yml       # Create domains, enroll servers
$ANSIBLE_CMD ad-parent_domain.yml # Parent domain setup
$ANSIBLE_CMD ad-child_domain.yml  # Child domain setup
sleep 5m                          # Allow replication
$ANSIBLE_CMD ad-members.yml       # Domain member enrollment
$ANSIBLE_CMD ad-trusts.yml        # Trust relationships
$ANSIBLE_CMD ad-data.yml          # Users, groups, OUs
$ANSIBLE_CMD ad-gmsa.yml          # Group Managed Service Accounts
$ANSIBLE_CMD laps.yml             # LAPS configuration
$ANSIBLE_CMD ad-relations.yml     # ACE/ACL relationships
$ANSIBLE_CMD adcs.yml             # AD Certificate Services
$ANSIBLE_CMD ad-acl.yml           # ACL attack paths
$ANSIBLE_CMD servers.yml          # IIS and MSSQL config
$ANSIBLE_CMD security.yml         # Defender and security settings
$ANSIBLE_CMD vulnerabilities.yml  # Intentional vulnerabilities
$ANSIBLE_CMD reboot.yml           # Final reboot
```

## Step 5: Validate

```bash
# Quick validation of key vulnerabilities
dreadgoad validate --quick --env staging --region us-west-1

# Full validation
dreadgoad validate --env staging --region us-west-1
```

## Host Mapping Reference

| Host | Computer Name | GOAD ID | Domain | OS | AMI Template |
|------|--------------|---------|--------|----|-------------|
| kingslanding | DC01 | dc01 | sevenkingdoms.local | 2019 | goad-dc-base |
| winterfell | DC02 | dc02 | north.sevenkingdoms.local | 2019 | goad-dc-base |
| meereen | DC03 | dc03 | essos.local | 2016 | goad-dc-base-2016 |
| castelblack | SRV02 | srv02 | north.sevenkingdoms.local | 2019 | goad-mssql-base |
| braavos | SRV03 | srv03 | essos.local | 2016 | (see note above) |

## Rebuilding AMIs

When you need to update the golden AMIs (e.g., for new Windows patches):

1. Rebuild with warpgate: `warpgate build goad-dc-base --target ami`
2. Update the AMI IDs in the relevant `terragrunt.hcl` files
3. Redeploy affected instances: `terragrunt apply` in each host directory
4. Re-run Ansible provisioning for the replaced instances

## Troubleshooting

**AMI not found**: Ensure `windows_ami_owners = ["self"]` is set and you built the AMI in the same region and AWS account.

**SSM connection fails**: Check that VPC endpoints for `ssm`, `ssmmessages`, and `ec2messages` are configured (the network module handles this automatically).

**Ansible timeouts**: Windows instances can take 5-10 minutes to fully boot and initialize SSM. If provisioning fails on first attempt, wait and retry.

**Terragrunt dependency errors**: Always deploy the `network` module before host modules. Use `terragrunt run-all` from the `goad/` directory to handle ordering automatically.
