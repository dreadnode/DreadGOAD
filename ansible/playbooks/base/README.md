# GOAD Base Image Playbooks

These playbooks are used by [warpgate](https://github.com/CowDogMoo/warpgate) to provision base AMI images for GOAD lab deployments.

## Purpose

Instead of installing software at runtime (which is slow), these playbooks pre-bake common software into AMIs:

- PowerShell DSC modules
- Windows Server roles (AD DS, IIS, SQL Server)
- RDP configuration
- Windows Updates
- Cleanup for AMI snapshots

This reduces GOAD deployment time from **60-90 minutes** to **15-30 minutes**.

## Playbooks

### `dc_base.yml` - Domain Controller Base

Provisions Windows Server with:

- PowerShell DSC modules (ActiveDirectoryDsc, ComputerManagementDsc, xNetworking, NetworkingDsc, xDnsServer)
- AD Domain Services role (NOT promoted - promotion happens at runtime)
- DNS Server role
- RSAT tools (AD, DNS, GPMC)
- RDP enabled
- Windows Updates
- SSM agent configuration for post-DC-promotion

**Used by**: `goad-dc-base` and `goad-dc-base-2016` warpgate templates

**Runtime tasks still needed**:

- DC promotion (`microsoft.ad.domain`)
- Domain-specific DNS configuration
- Trust relationships
- User/Group/OU creation

### `mssql_base.yml` - SQL Server Base

Provisions Windows Server with:

- All of member_base.yml content
- SQL Server Express 2019
- SQL Server firewall rules (TCP 1433, UDP 1434)
- Basic SQL Server configuration

**Used by**: `goad-mssql-base` and `goad-mssql-base-2016` warpgate templates

**Runtime tasks still needed**:

- Domain join
- SQL Server domain authentication
- SQL logins for domain users
- Database creation
- Linked servers

### `member_base.yml` - Member Server Base

Provisions Windows Server with:

- PowerShell DSC modules (ComputerManagementDsc, ActiveDirectoryDsc, xNetworking, NetworkingDsc)
- IIS web server + WebDAV
- RDP enabled
- Windows Updates

**Used by**: `goad-member-base` and `goad-member-base-2016` warpgate templates

**Runtime tasks still needed**:

- Domain join
- Member server roles (IIS apps, ADCS, etc.)
- GPO application
- Vulnerability injection

## Usage

### From Warpgate Templates

These playbooks are invoked by warpgate during AMI builds:

```yaml
# In templates/goad-dc-base/warpgate.yaml
provisioners:
  - type: ansible
    playbook_path: ${PROVISION_REPO_PATH}/playbooks/base/dc_base.yml
    galaxy_file: ${PROVISION_REPO_PATH}/requirements.yml
    extra_vars:
      ansible_connection: aws_ssm
      ansible_shell_type: powershell
      ansible_aws_ssm_bucket_name: ""
      ansible_aws_ssm_region: "{{.Variables.aws_region}}"
```

The `PROVISION_REPO_PATH` environment variable points to the DreadGOAD repository location.

### Manually (for testing)

```bash
# Set up inventory with Windows target via SSM
# Note: Instance must have SSM agent and be managed by Systems Manager
cat > inventory.ini <<EOF
[windows]
i-1234567890abcdef0  # Use EC2 instance ID, not IP

[windows:vars]
ansible_connection=aws_ssm
ansible_shell_type=powershell
ansible_aws_ssm_bucket_name=""
ansible_aws_ssm_region=us-west-1
EOF

# Ensure AWS credentials are configured
export AWS_PROFILE=your-profile
# or
export AWS_ACCESS_KEY_ID=xxx
export AWS_SECRET_ACCESS_KEY=xxx

# Install requirements
ansible-galaxy collection install -r requirements.yml

# Install boto3 for SSM connection plugin
pip3 install boto3 botocore

# Run playbook
ansible-playbook -i inventory.ini playbooks/base/dc_base.yml
```

## Connection Requirements

These playbooks expect:

- **Windows target** with SSM agent running (pre-installed on AWS AMIs)
- **AWS credentials** with SSM permissions in the build environment
- **Network connectivity** to download:
  - PowerShell modules from PowerShell Gallery
  - Windows Updates from Microsoft
  - SQL Server Express installer (for mssql_base.yml)

## Variables

These playbooks intentionally avoid requiring variables. They use sensible defaults for base image provisioning.

The only configurable variable is in `mssql_base.yml`:

```yaml
vars:
  sql_download_url: "https://go.microsoft.com/fwlink/p/?linkid=866658"  # SQL Server Express 2019
  sql_instance_name: "SQLEXPRESS"
```

## Design Decisions

### Why Not Domain Join in Base Images?

Domain join requires:

- Active Directory domain to exist
- Domain-specific DNS servers
- Computer name assignment
- OU placement

These are environment-specific and happen at runtime.

### Why Not Install Everything?

We only install software that:

1. Takes significant time (10+ minutes)
2. Is common across all GOAD deployments
3. Doesn't require domain membership
4. Doesn't change between environments

Examples of what's NOT included:

- ADCS (requires domain)
- LAPS (requires domain)
- Vulnerability injection (lab-specific)
- User accounts (lab-specific)

### Why Separate Playbooks?

Three separate playbooks instead of one parameterized playbook because:

- Clear separation of concerns
- Easier to maintain
- Faster execution (no conditional logic)
- Explicit about what each AMI contains

## Workflow Integration

These playbooks are part of the warpgate template build pipeline:

1. **GitHub workflow** clones DreadGOAD repo
2. **Warpgate** launches EC2 instance from AWS Windows base AMI
3. **Ansible** provisions the instance using these playbooks
4. **Packer/Warpgate** snapshots the instance to create custom AMI
5. **Terragrunt** launches GOAD instances from custom AMIs
6. **Runtime ansible** completes domain-specific configuration

## Maintenance

When updating these playbooks:

1. Test manually against a fresh Windows Server instance
2. Verify AMI builds in warpgate CI/CD
3. Confirm GOAD runtime deployment still works
4. Update this README if behavior changes

## Related Files

- `../../roles/common/` - Runtime common configuration
- `../../roles/domain_controller/` - Runtime DC promotion
- `../../roles/member_server/` - Runtime domain join
- `../../roles/mssql/` - Runtime SQL Server configuration
- `../../../warpgate-templates/templates/goad-*` - Warpgate template definitions
- `.github/workflows/build-and-push-templates.yaml` - AMI build pipeline
