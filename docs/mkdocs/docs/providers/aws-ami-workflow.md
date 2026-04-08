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

- [warpgate](https://github.com/cowdogmoo/warpgate) CLI installed (v4.3+)
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

The repo ships with a `staging` directory tree. To create a new environment (e.g., `dev`), use the CLI:

```bash
dreadgoad env create dev
```

This scaffolds the full Terragrunt tree, pulling the VPC CIDR from your
`dreadgoad.yaml` config (`environments.dev.vpc_cidr`). You can also pass
`--vpc-cidr` explicitly. Each environment gets its own Terraform state,
so you can run multiple labs in parallel.

Throughout this guide, examples use `staging` and `us-west-1` to match the defaults. Replace with your chosen env and region as needed.

## Step 1: Build Golden AMIs with Warpgate

DreadGOAD provides four warpgate templates under `warpgate-templates/`:

| Template | Target Hosts | OS | Saves |
|----------|-------------|-----|-------|
| `goad-dc-base` | DC01, DC02 | Windows Server 2019 | ~25 min/host |
| `goad-dc-base-2016` | DC03 | Windows Server 2016 | ~25 min/host |
| `goad-mssql-base` | SRV02 | Windows Server 2019 | ~48 min/host |
| `goad-mssql-base-2016` | SRV03 (optional) | Windows Server 2016 | ~20 min/host |

### Build all AMIs

```bash
# Build all templates in parallel
dreadgoad ami build --all

# Or build individually
dreadgoad ami build goad-dc-base
dreadgoad ami build goad-dc-base-2016
dreadgoad ami build goad-mssql-base
dreadgoad ami build goad-mssql-base-2016
```

To build for a specific region:

```bash
dreadgoad ami build goad-dc-base --region us-west-1
```

You can also call `warpgate` directly for more control:

```bash
warpgate build goad-dc-base --target ami --region us-west-1
```

### AMI resolution is automatic

Each warpgate template tags its output AMI with `Name: <template-name>` (e.g., `Name: goad-dc-base`). The terragrunt host configurations filter by this tag with `most_recent = true`, so they always pick up the latest build automatically. No manual AMI ID tracking is needed.

!!! note "SRV03 (braavos)"
    SRV03 runs Windows Server 2016 as a member server. You can use the dedicated `goad-mssql-base-2016` AMI, or alternatively `goad-dc-base-2016` (the extra AD DS role won't interfere) or a vanilla Windows Server 2016 AMI.

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

### How AMI selection works

Each host's `terragrunt.hcl` already contains the correct AMI filter. For example, `dc01/terragrunt.hcl`:

```hcl
windows_ami_owners = ["self"]

additional_windows_ami_filters = [
  {
    name   = "tag:Name"
    values = ["goad-dc-base"]
  }
]
```

The instance-factory module uses `most_recent = true`, so after building a new AMI with `dreadgoad ami build`, the next `dreadgoad infra apply` automatically picks it up.

| Template | Hosts | Filter Tag |
|----------|-------|------------|
| `goad-dc-base` | DC01, DC02 | `tag:Name = goad-dc-base` |
| `goad-dc-base-2016` | DC03 | `tag:Name = goad-dc-base-2016` |
| `goad-mssql-base` | SRV02 | `tag:Name = goad-mssql-base` |
| `goad-mssql-base-2016` | SRV03 | `tag:Name = goad-mssql-base-2016` |

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
dreadgoad infra init --env staging --region us-west-1
dreadgoad infra apply --env staging --region us-west-1
```

Or with raw Terragrunt for more control:

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
    `terragrunt run-all` (and `dreadgoad infra apply`) deploys DC01-DC03, SRV02, and SRV03 in parallel. The dependency on the network module is resolved automatically.

### Verify instances

All instances use SSM for management -- no SSH keys or open ports required:

```bash
dreadgoad health-check --env staging --region us-west-1
```

Or with the AWS CLI directly:

```bash
# Check instance status
aws ec2 describe-instances \
  --filters "Name=tag:Project,Values=DreadGOAD" \
  --query "Reservations[].Instances[].[Tags[?Key=='Name'].Value|[0],State.Name,InstanceId]" \
  --output table

# Connect to an instance via SSM
aws ssm start-session --target <instance-id>
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
| braavos | SRV03 | srv03 | essos.local | 2016 | goad-mssql-base-2016 (optional) |

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

**Provisioning fails mid-run**: This is normal — stop with `Ctrl+C`, fix the issue (inventory, playbook, etc.), and resume with `--from`:

```bash
dreadgoad provision --env staging --region us-west-1 --from ad-trusts.yml
```

The CLI re-reads all configuration on each run, so your fixes are picked up immediately. See the [Stopping, Fixing, and Resuming](../provisioning.md#stopping-fixing-and-resuming-provisioning) section for the full workflow.
