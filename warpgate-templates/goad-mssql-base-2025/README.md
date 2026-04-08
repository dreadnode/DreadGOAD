# goad-mssql-base-2025

Pre-baked Windows Server 2025 AMI with MSSQL Express 2022, IIS, and Windows Updates pre-installed for GOAD member servers (DRACARYS lab).

## Purpose

This template creates a "golden" AMI for **Windows Server 2025** that significantly reduces GOAD deployment time by pre-installing:

- Windows Updates (saves ~15 minutes per instance)
- MSSQL Express 2022 (saves ~25 minutes per instance)
- IIS with WebDAV (saves ~5 minutes per instance)
- Required PowerShell DSC modules
- SQL Server firewall rules

Uses SQL Server Express **2022** (not 2019) because SQL Server 2019 does not support Windows Server 2025.

**Note**: MSSQL is installed with a temporary sa password. Domain-specific configuration (sysadmins, linked servers, impersonation) happens at runtime.

## Usage

### Build the AMI

```bash
warpgate build goad-mssql-base-2025 --target ami
```

### Update Terragrunt

For DRACARYS SRV01:

```hcl
inputs = {
  windows_os         = "Windows_Server"
  windows_os_version = "2025-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "tag:Name"
      values = ["goad-mssql-base-2025"]
    }
  ]

  windows_ami_owners = ["self"]
}
```

## Tags

The AMI is tagged with:

- `Name`: goad-mssql-base-2025
- `Lab`: GOAD
- `Role`: MemberServer
- `Software`: MSSQL-Express-2022
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2025
