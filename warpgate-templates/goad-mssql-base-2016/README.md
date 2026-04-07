# goad-mssql-base-2016

Pre-baked Windows Server 2016 AMI with MSSQL Express 2019, IIS, and Windows Updates pre-installed for GOAD member servers.

## Purpose

This template creates a "golden" AMI for **Windows Server 2016** that significantly reduces GOAD deployment time by pre-installing:

- Windows Updates (saves ~15 minutes per instance)
- MSSQL Express 2019 (saves ~25 minutes per instance)
- IIS with WebDAV (saves ~5 minutes per instance)
- Required PowerShell DSC modules
- SQL Server firewall rules

**Note**: MSSQL is installed with a temporary sa password. Domain-specific configuration (sysadmins, linked servers, impersonation) happens at runtime.

## Usage

### Build the AMI

```bash
warpgate build goad-mssql-base-2016 --target ami
```

### Update Terragrunt

For SRV03 (uses 2016):

```hcl
inputs = {
  windows_os         = "Windows_Server"
  windows_os_version = "2016-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "image-id"
      values = ["ami-xxxxxxxxxxxx"]  # Your goad-mssql-base-2016 AMI ID
    }
  ]

  windows_ami_owners = ["self"]
}
```

## Tags

The AMI is tagged with:

- `Name`: goad-mssql-base-2016
- `Lab`: GOAD
- `Role`: MemberServer
- `Software`: MSSQL-Express-2019
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2016
