# goad-member-base-2016

Pre-baked Windows Server 2016 AMI with IIS/WebDAV and Windows Updates pre-installed for GOAD member servers.

## Purpose

This template creates a "golden" AMI for member servers (non-domain-controllers) running Windows Server 2016. It pre-installs:

- Windows Updates (saves ~15 minutes per instance)
- IIS web server with WebDAV
- Required PowerShell DSC modules
- RDP enabled

## Time Savings

| Component | Vanilla AMI | Pre-baked AMI |
| --------- | ----------- | ------------- |
| Windows Updates | ~15 min | 0 min |
| IIS/WebDAV Install | ~3 min | 0 min |
| DSC Modules | ~2 min | 0 min |
| **Total per host** | **~20 min** | **0 min** |

## Usage

### Build the AMI

```bash
# Build using warpgate CLI
warpgate build goad-member-base-2016 --target ami

# Or with custom region
warpgate build goad-member-base-2016 --target ami --region us-east-1
```

### Use in Terragrunt

Update your GOAD terragrunt.hcl for SRV03:

```hcl
inputs = {
  # Windows AMI configuration - using pre-baked goad-member-base-2016 AMI
  windows_os         = "Windows_Server"
  windows_os_version = "2016-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "image-id"
      values = ["ami-xxxxxxxxxxxx"]  # Your goad-member-base-2016 AMI ID
    }
  ]

  windows_ami_owners = ["self"]
}
```

## What's Pre-installed

### Windows Features

- IIS web server
- WebDAV

### PowerShell Modules

- ComputerManagementDsc
- ActiveDirectoryDsc
- xNetworking
- NetworkingDsc
- PSWindowsUpdate

### Configuration

- RDP enabled
- Windows Updates applied

## What Still Needs to Run at Deployment

The following must still be configured at deployment time (domain-specific):

1. Domain join (`ad-members.yml`)
2. IIS application configuration (`servers.yml`)
3. GPO configuration
4. ADCS roles (if applicable)
5. Vulnerability injection

## Tags

The AMI is tagged with:

- `Name`: goad-member-base-2016
- `Lab`: GOAD
- `Role`: MemberServer
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2016
