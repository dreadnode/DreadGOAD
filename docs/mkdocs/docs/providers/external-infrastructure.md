# External Infrastructure

This guide covers using DreadGOAD provisioning when EC2 instances are created by an external system (e.g., a separate Terraform stack, a CI/CD pipeline, or a custom orchestrator) rather than by DreadGOAD's built-in Terragrunt modules.

## Overview

```text
dreadgoad ami build --all (golden AMIs)
        |
        v
external system creates EC2 instances using those AMIs
        |
        v
dreadgoad inventory sync (discovers instances, updates inventory)
        |
        v
dreadgoad provision (runs Ansible to configure AD lab)
```

In this workflow, DreadGOAD is responsible for two things: building the golden AMIs that your instances boot from, and running the Ansible provisioning that turns those instances into a functioning Active Directory lab. The infrastructure itself -- VPCs, subnets, security groups, EC2 instances -- is managed elsewhere.

## Prerequisites

- [warpgate](https://github.com/cowdogmoo/warpgate) CLI installed (v4.3+) for building AMIs
- [AWS CLI](https://aws.amazon.com/cli/) configured with appropriate credentials
- [Ansible](https://docs.ansible.com/) >= 2.15
- Go 1.21+ (for the `dreadgoad` CLI)
- The `amazon.aws` Ansible collection installed

## Requirements for External Infrastructure

Your externally-managed infrastructure must satisfy several requirements for DreadGOAD's inventory sync and Ansible provisioning to work.

### Instance Name Tags

The `dreadgoad inventory sync` command discovers instances by querying EC2 for Name tags matching the pattern `*<env>*dreadgoad*`. The hostname is extracted from the portion after `dreadgoad-` in the tag value.

Your instances **must** have a Name tag that:

1. Contains both the environment name and the string `dreadgoad`
2. Ends with `dreadgoad-<hostname>` where `<hostname>` matches the inventory hostname

For example, with `--env staging`:

| Instance | Required Name Tag (example) | Extracted Hostname |
|----------|---------------------------|-------------------|
| DC01 | `staging-mydeployment-dreadgoad-DC01` | `dc01` |
| DC02 | `staging-mydeployment-dreadgoad-DC02` | `dc02` |
| DC03 | `staging-mydeployment-dreadgoad-DC03` | `dc03` |
| SRV02 | `staging-mydeployment-dreadgoad-SRV02` | `srv02` |
| SRV03 | `staging-mydeployment-dreadgoad-SRV03` | `srv03` |

The prefix before `dreadgoad-` can be anything, but it must contain the environment name (e.g., `staging`). The hostname after `dreadgoad-` is lowercased and matched against the inventory file.

!!! warning "Tag format is strict"
    The delimiter is `dreadgoad-` (with a trailing hyphen). A tag like `staging-dreadgoad_DC01` or `staging-dreadgoadDC01` will **not** be matched. Use `dreadgoad-DC01`.

### SSM Connectivity

DreadGOAD uses AWS Systems Manager (SSM) as the Ansible connection plugin -- there is no SSH or WinRM involved. Every instance must:

- Have the **SSM Agent** installed and running (pre-baked AMIs include this)
- Have an **IAM instance profile** with the `AmazonSSMManagedInstanceCore` policy (or equivalent)
- Be in a **VPC with SSM endpoints** configured

The required VPC endpoints are:

| Endpoint | Service |
|----------|---------|
| `com.amazonaws.<region>.ssm` | Systems Manager API |
| `com.amazonaws.<region>.ssmmessages` | Session Manager |
| `com.amazonaws.<region>.ec2messages` | EC2 message delivery |

!!! tip
    If your instances can reach the internet (via NAT gateway or internet gateway), VPC endpoints are not strictly required -- SSM will connect through the public endpoints. VPC endpoints are needed for fully private subnets.

### S3 Bucket for SSM File Transfers

Ansible's SSM connection plugin transfers files via an S3 bucket. You must create an S3 bucket and reference it in the inventory file as `ansible_aws_ssm_bucket_name`.

The bucket must:

- Be in the same region as the instances
- Allow read/write access from the instances' IAM role
- Allow read/write access from the machine running Ansible

### Instance Configuration

Each instance should:

- Boot from a DreadGOAD golden AMI (built with `dreadgoad ami build`)
- Have a local `ansible` user that Ansible connects as (the golden AMIs create this user)
- Be in a **running** state when inventory sync and provisioning are executed

### Network Requirements

Instances must be able to communicate with each other on the private network. The GOAD lab requires:

- All instances in the same VPC (or peered VPCs with routing)
- Security groups allowing all traffic between lab instances (AD requires many ports)
- DNS resolution between instances

## Step 1: Build Golden AMIs

Build the pre-baked AMIs that your external system will use to launch instances. These AMIs include Windows Updates, AD DS roles, MSSQL, SSM agent configuration, and other dependencies.

```bash
# Build all four AMI templates in parallel
dreadgoad ami build --all

# Or build individually for a specific region
dreadgoad ami build goad-dc-base --region us-west-1
dreadgoad ami build goad-dc-base-2016 --region us-west-1
dreadgoad ami build goad-mssql-base --region us-west-1
dreadgoad ami build goad-mssql-base-2016 --region us-west-1
```

Each template produces an AMI tagged with `Name: <template-name>`. Your external system should reference these AMIs by tag or by AMI ID.

| Template | Target Hosts | OS | AMI Tag |
|----------|-------------|-----|---------|
| `goad-dc-base` | DC01, DC02 | Windows Server 2019 | `Name: goad-dc-base` |
| `goad-dc-base-2016` | DC03 | Windows Server 2016 | `Name: goad-dc-base-2016` |
| `goad-mssql-base` | SRV02 | Windows Server 2019 | `Name: goad-mssql-base` |
| `goad-mssql-base-2016` | SRV03 | Windows Server 2016 | `Name: goad-mssql-base-2016` |

For details on what is pre-baked in each AMI versus what Ansible configures at runtime, see the [AMI Workflow](aws-ami-workflow.md#whats-pre-baked-vs-runtime) documentation.

## Step 2: Create Instances Externally

Launch EC2 instances using your external system. Ensure each instance meets the requirements described above. As a reference, here is what your external system needs to produce:

```text
5 EC2 instances:
  - DC01  -> goad-dc-base AMI,      t2.medium, Name tag: "<env>-<prefix>-dreadgoad-DC01"
  - DC02  -> goad-dc-base AMI,      t2.medium, Name tag: "<env>-<prefix>-dreadgoad-DC02"
  - DC03  -> goad-dc-base-2016 AMI, t2.medium, Name tag: "<env>-<prefix>-dreadgoad-DC03"
  - SRV02 -> goad-mssql-base AMI,   t2.medium, Name tag: "<env>-<prefix>-dreadgoad-SRV02"
  - SRV03 -> goad-mssql-base-2016,  t2.medium, Name tag: "<env>-<prefix>-dreadgoad-SRV03"

VPC with:
  - Private subnets
  - SSM, SSMMessages, EC2Messages VPC endpoints (or internet access)
  - Security group allowing all inter-instance traffic

IAM instance profile with:
  - AmazonSSMManagedInstanceCore policy
  - S3 read/write to the SSM bucket

S3 bucket for SSM file transfers
```

!!! note "Instance type"
    `t2.medium` (2 vCPU, 4 GB RAM) is the minimum recommended size. Domain controllers and MSSQL servers benefit from `t3.medium` or larger for faster provisioning.

## Step 3: Prepare the Inventory File

DreadGOAD resolves the inventory file as `<env>-inventory` in the project root. For example, `--env staging` uses `staging-inventory`.

If you do not already have an inventory file, copy the example and adjust it:

```bash
cp staging-inventory.example myenv-inventory
```

Edit the inventory to set:

- `env=<your-env>` in the `[all:vars]` section
- `ansible_aws_ssm_bucket_name=<your-bucket>` to your S3 bucket
- `ansible_aws_ssm_region=<your-region>` to the region where instances run

The `ansible_host` values for each host will be populated automatically by `inventory sync` in the next step. You can leave them as placeholder values initially.

Here is the relevant section of the inventory file:

```ini
[all:vars]
domain_name=GOAD
admin_user=administrator
env=staging

; SSM connection (windows)
ansible_become=false
ansible_connection=amazon.aws.aws_ssm
ansible_aws_ssm_bucket_name=your-ssm-bucket-name
ansible_aws_ssm_region=us-west-1
ansible_shell_type=powershell
ansible_aws_ssm_s3_addressing_style=virtual
ansible_aws_ssm_retries=3
ansible_remote_tmp=C:\Windows\Temp

[default]
dc01 ansible_host=PLACEHOLDER dict_key=dc01 dns_domain=dc01 ansible_user=ansible
dc02 ansible_host=PLACEHOLDER dict_key=dc02 dns_domain=dc01 ansible_user=ansible
srv02 ansible_host=PLACEHOLDER dict_key=srv02 dns_domain=dc02 ansible_user=ansible
dc03 ansible_host=PLACEHOLDER dict_key=dc03 dns_domain=dc03 ansible_user=ansible
srv03 ansible_host=PLACEHOLDER dict_key=srv03 dns_domain=dc03 ansible_user=ansible
```

!!! info "Full inventory structure"
    The inventory file contains additional group definitions (`[domain]`, `[dc]`, `[server]`, `[trust]`, etc.) that define the lab topology. These do not need to change for external infrastructure -- only the `[all:vars]` connection settings and the `ansible_host` values in `[default]` need updating.

## Step 4: Sync Inventory

Once your external instances are running, use `inventory sync` to discover them and update the inventory file with their instance IDs:

```bash
dreadgoad inventory sync --env staging --region us-west-1
```

This command:

1. Queries EC2 for instances matching the Name tag pattern `*staging*dreadgoad*`
2. Extracts the hostname from the portion after `dreadgoad-` in each Name tag
3. Updates the `ansible_host` field for each matching host in the inventory file

You should see output like:

```text
Updated dc01 with instance ID: i-01c4c6e97d44a1ec3
Updated dc02 with instance ID: i-06714badabb0a1ec4
Updated dc03 with instance ID: i-0f5b5bb47f44940af
Updated srv02 with instance ID: i-0a2b5c0ca7553b230
Updated srv03 with instance ID: i-0f9c3af8127a99f05
Updated 5 instance IDs in staging-inventory
```

!!! tip "Backup option"
    Use `--backup` to create a timestamped backup of the inventory file before modifying it:
    ```bash
    dreadgoad inventory sync --env staging --region us-west-1 --backup
    ```

### Alternative: JSON input

If your external system can produce a JSON file with instance data, you can skip the AWS discovery and provide it directly:

```bash
dreadgoad inventory sync --env staging --json instances.json
```

The JSON file should be an array of objects with `InstanceId` and `Name` fields:

```json
[
  {"InstanceId": "i-01c4c6e97d44a1ec3", "Name": "staging-mydeployment-dreadgoad-DC01"},
  {"InstanceId": "i-06714badabb0a1ec4", "Name": "staging-mydeployment-dreadgoad-DC02"},
  {"InstanceId": "i-0f5b5bb47f44940af", "Name": "staging-mydeployment-dreadgoad-DC03"},
  {"InstanceId": "i-0a2b5c0ca7553b230", "Name": "staging-mydeployment-dreadgoad-SRV02"},
  {"InstanceId": "i-0f9c3af8127a99f05", "Name": "staging-mydeployment-dreadgoad-SRV03"}
]
```

### Verify the inventory

After syncing, confirm the inventory looks correct:

```bash
dreadgoad inventory show --env staging
```

This displays each host, its instance ID, and group membership.

## Step 5: Provision the AD Lab

With the inventory populated, run provisioning to configure the Active Directory environment:

```bash
dreadgoad provision --env staging --region us-west-1
```

Provisioning runs the full Ansible playbook sequence: domain creation, trust relationships, user/group provisioning, ADCS, LAPS, vulnerability injection, and more. This takes approximately 60-90 minutes depending on instance size and network conditions.

!!! tip "Resume after failure"
    If provisioning fails at a specific playbook, fix the issue and resume from where it left off:
    ```bash
    dreadgoad provision --env staging --region us-west-1 --from ad-trusts.yml
    ```
    See [Stopping, Fixing, and Resuming](../provisioning.md#stopping-fixing-and-resuming-provisioning) for the full workflow.

### Validate

After provisioning completes, validate the lab:

```bash
dreadgoad validate --env staging --region us-west-1
```

## Complete Workflow Summary

```bash
# 1. Build golden AMIs (one-time, reuse across deployments)
dreadgoad ami build --all --region us-west-1

# 2. [External system creates EC2 instances using the AMIs]

# 3. Sync inventory with discovered instance IDs
dreadgoad inventory sync --env staging --region us-west-1

# 4. Provision the AD lab
dreadgoad provision --env staging --region us-west-1

# 5. Validate
dreadgoad validate --env staging --region us-west-1
```

## Differences from Built-in Terragrunt Workflow

| Aspect | Built-in Terragrunt | External Infrastructure |
|--------|---------------------|------------------------|
| Infrastructure creation | `dreadgoad infra apply` | Your external system |
| VPC/networking | Managed by Terragrunt network module | You manage this |
| SSM VPC endpoints | Created by Terragrunt automatically | You must create these |
| S3 bucket | Created by Terragrunt | You must create this |
| IAM roles | Created by Terragrunt | You must create these |
| Instance tagging | Automatic | You must tag correctly |
| AMI selection | Automatic via Terragrunt filters | You select AMIs manually |
| Inventory sync | Same: `dreadgoad inventory sync` | Same |
| Provisioning | Same: `dreadgoad provision` | Same |

## Troubleshooting

**Inventory sync finds no instances**: Verify that your instance Name tags match the expected pattern. The query uses `*<env>*dreadgoad*` -- for `--env staging`, tags must contain both `staging` and `dreadgoad`. Check with:

```bash
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=*staging*dreadgoad*" \
  --query "Reservations[].Instances[].[InstanceId,Tags[?Key=='Name'].Value|[0],State.Name]" \
  --output table
```

**Inventory sync runs but updates 0 hosts**: The hostname extracted from the tag (the part after `dreadgoad-`) must match a hostname in your inventory file. For example, if the tag ends in `dreadgoad-DC01`, the inventory must have a line starting with `dc01` (case-insensitive match). Check for typos or mismatched hostnames.

**SSM connection fails during provisioning**: Verify that instances have the SSM agent running and an IAM instance profile with SSM permissions. Test connectivity with:

```bash
# Check SSM agent status
aws ssm describe-instance-information \
  --filters "Key=InstanceIds,Values=i-01c4c6e97d44a1ec3" \
  --query "InstanceInformationList[].[InstanceId,PingStatus,AgentVersion]" \
  --output table

# Test SSM session
aws ssm start-session --target i-01c4c6e97d44a1ec3
```

**S3 permission errors during provisioning**: Ansible's SSM connection plugin reads and writes files through S3. Ensure the `ansible_aws_ssm_bucket_name` in the inventory matches an existing bucket, and that both the instances' IAM role and your local AWS credentials have read/write access to it.

**Ansible timeouts on first playbook**: Windows instances can take 5-10 minutes to fully boot and initialize the SSM agent after launch. If the first provisioning attempt fails with connection timeouts, wait a few minutes and retry.

**Provisioning fails mid-run**: This is normal. Use `--from` to resume from the failed playbook. The CLI includes automatic retry logic with error-specific strategies (session cleanup, reboot handling, etc.). See the [provisioning documentation](../provisioning.md#stopping-fixing-and-resuming-provisioning) for details.
