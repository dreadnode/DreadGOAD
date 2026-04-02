# goad-dc-base

Pre-baked Windows Server 2019 AMI with AD DS role and Windows Updates pre-installed for GOAD domain controllers.

## Purpose

This template creates a "golden" AMI that significantly reduces GOAD deployment time by pre-installing:

- Windows Updates (saves ~15 minutes per instance)
- AD-Domain-Services role (NOT promoted - promotion happens at runtime)
- DNS Server role
- RSAT tools (RSAT-AD-Tools, RSAT-DNS-Server, RSAT-ADDS)
- Group Policy Management Console (GPMC)
- Required PowerShell DSC modules
- SSM agent configuration for post-DC-promotion survival

**Note**: The AD DS role is installed but NOT promoted to a domain controller. Domain promotion with domain-specific settings happens at runtime via Ansible.

## Time Savings

| Component | Vanilla AMI | Pre-baked AMI |
| --------- | ----------- | ------------- |
| Windows Updates | ~15 min | 0 min |
| AD DS Role Install | ~5 min | 0 min |
| DNS Role Install | ~2 min | 0 min |
| DSC Modules | ~3 min | 0 min |
| **Total per DC** | **~25 min** | **0 min** |

With 3 domain controllers in GOAD, this saves approximately **75 minutes** per deployment.

## Usage

### Build the AMI

```bash
# Build using warpgate CLI
warpgate build goad-dc-base --target ami

# Or with custom region
warpgate build goad-dc-base --target ami --vars aws_region=us-east-1
```

### Use in Terragrunt

Update your GOAD terragrunt.hcl for DC01/DC02/DC03:

```hcl
inputs = {
  # Windows AMI configuration - using pre-baked goad-dc-base AMI
  windows_os         = "Windows_Server"
  windows_os_version = "2019-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "image-id"
      values = ["ami-xxxxxxxxxxxx"]  # Your goad-dc-base AMI ID
    }
  ]

  windows_ami_owners = ["self"]
}
```

## What's Pre-installed

### Windows Features

- AD-Domain-Services (not promoted)
- DNS
- RSAT-AD-Tools
- RSAT-DNS-Server
- RSAT-ADDS
- GPMC (Group Policy Management Console)

### PowerShell Modules

- ComputerManagementDsc
- ActiveDirectoryDsc
- xNetworking
- NetworkingDsc
- PSWindowsUpdate

### Configuration

- RDP enabled
- Windows Updates applied
- SSM agent configured with scheduled task for post-DC-promotion restart

## What Still Needs to Run at Deployment

The following must still be configured at deployment time (domain-specific):

1. AD domain promotion (`ad-parent_domain.yml`, `ad-child_domain.yml`)
2. User/group creation (`ad-data.yml`)
3. Trust relationships (`ad-trusts.yml`)
4. GPO configuration
5. LAPS installation
6. Vulnerability injection

## Tags

The AMI is tagged with:

- `Name`: goad-dc-base
- `Lab`: GOAD
- `Role`: DomainController
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2019
