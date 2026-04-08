# Warpgate Templates

Pre-baked AMI templates for DreadGOAD, built with [warpgate](https://github.com/cowdogmoo/warpgate). These templates create "golden" AMIs that significantly reduce deployment time by pre-installing Windows Updates, server roles, and dependencies.

## Templates

### GOAD Lab

| Template | OS | Pre-installed Software | Target Hosts | Time Saved |
|----------|-----|----------------------|--------------|------------|
| [goad-dc-base](goad-dc-base/) | Windows Server 2019 | AD DS, DNS, RSAT, DSC modules, SSM | DC01, DC02 | ~25 min/host |
| [goad-dc-base-2016](goad-dc-base-2016/) | Windows Server 2016 | AD DS, DNS, RSAT, DSC modules, SSM | DC03 | ~25 min/host |
| [goad-mssql-base](goad-mssql-base/) | Windows Server 2019 | MSSQL Express 2019, IIS/WebDAV, DSC modules | SRV02 | ~48 min/host |
| [goad-mssql-base-2016](goad-mssql-base-2016/) | Windows Server 2016 | MSSQL Express 2019, IIS/WebDAV, DSC modules | SRV03 | ~48 min/host |

### DRACARYS Lab

| Template | OS | Pre-installed Software | Target Hosts | Time Saved |
|----------|-----|----------------------|--------------|------------|
| [goad-dc-base-2025](goad-dc-base-2025/) | Windows Server 2025 | AD DS, DNS, RSAT, DSC modules, SSM | DC01 | ~25 min/host |
| [goad-mssql-base-2025](goad-mssql-base-2025/) | Windows Server 2025 | MSSQL Express 2022, IIS/WebDAV, DSC modules | SRV01 | ~48 min/host |

Total time savings: **~171 minutes** per full GOAD deployment, **~73 minutes** per DRACARYS deployment.

## Prerequisites

- [warpgate](https://github.com/cowdogmoo/warpgate) CLI installed (v4.3+)
- AWS credentials configured with permissions to create EC2 instances and AMIs
- The DreadGOAD repo cloned (templates reference Ansible playbooks via `$PROVISION_REPO_PATH`)

## Quick Start

```bash
# Set the repo path for Ansible playbook references
export PROVISION_REPO_PATH=/path/to/DreadGOAD

# Build GOAD templates
warpgate build goad-dc-base --target ami
warpgate build goad-dc-base-2016 --target ami
warpgate build goad-mssql-base --target ami
warpgate build goad-mssql-base-2016 --target ami

# Build DRACARYS templates
warpgate build goad-dc-base-2025 --target ami
warpgate build goad-mssql-base-2025 --target ami

# Build for a specific region
warpgate build goad-dc-base --target ami --region us-east-1
```

Each build outputs an AMI ID (e.g., `ami-0abc1234def56789`). Record these for use in Terragrunt host configurations.

## How It Works

1. Warpgate launches a temporary EC2 instance from the base Windows AMI
2. Ansible provisions the instance via AWS SSM (no SSH required)
3. Warpgate snapshots the instance to create a custom AMI
4. The AMI is tagged with metadata (lab, role, base OS, etc.)

### Pre-baked vs. Runtime

AMIs contain only software that is slow to install and common across all deployments. Domain-specific configuration happens at runtime via Ansible:

| Pre-baked (in AMI) | Runtime (Ansible) |
|---|---|
| Windows Updates | AD domain promotion |
| AD DS role (unpromoted) | User/group creation |
| DNS, RSAT tools | Trust relationships |
| MSSQL Express (mssql-base) | GPO configuration |
| IIS/WebDAV (member/mssql) | LAPS, ADCS |
| PowerShell DSC modules | Vulnerability injection |
| SSM agent configuration | Domain joins |

## Template Structure

Each template contains:

```text
goad-{template-name}/
├── warpgate.yaml   # Template definition (base image, provisioners, targets)
└── README.md       # Template-specific documentation
```

The `warpgate.yaml` files reference Ansible playbooks under `ansible/playbooks/base/`:

- `dc_base.yml` -- used by `goad-dc-base`, `goad-dc-base-2016`, and `goad-dc-base-2025`
- `mssql_base_setup.yml` + `mssql_base_sql.yml` -- used by `goad-mssql-base`, `goad-mssql-base-2016`, and `goad-mssql-base-2025`

## Using Built AMIs

After building, insert the AMI IDs into your Terragrunt host configurations under `infra/goad-deployment/{env}/{region}/goad/{host}/terragrunt.hcl`. See the [AWS AMI build & deploy workflow](../docs/mkdocs/docs/providers/aws-ami-workflow.md) for the full end-to-end guide.

## Rebuilding AMIs

Rebuild when Windows patches are released or when base playbooks change:

```bash
warpgate build goad-dc-base --target ami
# Update AMI IDs in terragrunt.hcl files
# Redeploy affected instances with terragrunt apply
# Re-run Ansible provisioning
```
