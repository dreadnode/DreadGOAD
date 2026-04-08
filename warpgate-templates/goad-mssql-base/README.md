# goad-mssql-base

Pre-baked Windows Server 2019 AMI with MSSQL Express 2019, IIS, and Windows Updates pre-installed for GOAD member servers.

## Purpose

This template creates a "golden" AMI that significantly reduces GOAD deployment time by pre-installing:

- Windows Updates (saves ~15 minutes per instance)
- MSSQL Express 2019 (saves ~25 minutes per instance)
- IIS with WebDAV (saves ~5 minutes per instance)
- Required PowerShell DSC modules
- SQL Server firewall rules

**Note**: MSSQL is installed with a temporary sa password. Domain-specific configuration (sysadmins, linked servers, impersonation) happens at runtime.

## Time Savings

| Component | Vanilla AMI | Pre-baked AMI |
| --------- | ----------- | ------------- |
| Windows Updates | ~15 min | 0 min |
| MSSQL Express Install | ~25 min | 0 min |
| IIS Install | ~5 min | 0 min |
| DSC Modules | ~3 min | 0 min |
| **Total per server** | **~48 min** | **0 min** |

With 2 member servers in GOAD running MSSQL, this saves approximately **96 minutes** per deployment.

## Usage

### Build the AMI

```bash
# Build using warpgate CLI
warpgate build goad-mssql-base --target ami

# Or with custom region
warpgate build goad-mssql-base --target ami --vars aws_region=us-east-1
```

### Use in Terragrunt

Update your GOAD terragrunt.hcl for SRV02:

```hcl
inputs = {
  # Windows AMI configuration - using pre-baked goad-mssql-base AMI
  windows_os         = "Windows_Server"
  windows_os_version = "2019-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "image-id"
      values = ["ami-xxxxxxxxxxxx"]  # Your goad-mssql-base AMI ID
    }
  ]

  windows_ami_owners = ["self"]
}
```

## What's Pre-installed

### Software

- MSSQL Express 2019 (SQLEXPRESS instance)
- IIS with all subfeatures
- WebDAV

### Windows Features

- Web-Server (IIS)
- Web-DAV-Publishing

### PowerShell Modules

- ComputerManagementDsc
- ActiveDirectoryDsc
- xNetworking
- NetworkingDsc
- PSWindowsUpdate

### Configuration

- MSSQL listening on TCP port 1433
- SQL Browser service enabled
- Firewall rules for MSSQL (1433/TCP, 1434/UDP)
- RDP enabled
- Windows Updates applied

## What Still Needs to Run at Deployment

The following must still be configured at deployment time (domain-specific):

1. Domain join (`ad-members.yml`)
2. MSSQL domain account configuration:
   - Add domain sysadmins
   - Configure linked servers
   - Set up impersonation grants
   - Reset sa password
3. ADCS installation (for SRV03)
4. Vulnerability injection

## MSSQL Pre-configuration Details

The MSSQL instance is configured with:

- Instance name: `SQLEXPRESS`
- Service account: `NT AUTHORITY\NETWORK SERVICE`
- TCP enabled on port 1433
- Named pipes enabled
- SQL and Windows authentication mode
- Temporary sa password (must be changed at deployment)

## Tags

The AMI is tagged with:

- `Name`: goad-mssql-base
- `Lab`: GOAD
- `Role`: MemberServer
- `Software`: MSSQL-Express-2019
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2019
