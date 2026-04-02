# goad-dc-base-2016

Pre-baked Windows Server 2016 AMI with AD DS role and Windows Updates pre-installed for GOAD domain controllers.

## Purpose

This template creates a "golden" AMI for **Windows Server 2016** that significantly reduces GOAD deployment time by pre-installing:

- Windows Updates (saves ~15 minutes per instance)
- AD-Domain-Services role (NOT promoted - promotion happens at runtime)
- DNS Server role
- RSAT tools (RSAT-AD-Tools, RSAT-DNS-Server, RSAT-ADDS)
- Group Policy Management Console (GPMC)
- Required PowerShell DSC modules
- SSM agent configuration for post-DC-promotion survival

**Note**: The AD DS role is installed but NOT promoted to a domain controller. Domain promotion with domain-specific settings happens at runtime via Ansible.

## Usage

### Build the AMI

```bash
warpgate build goad-dc-base-2016 --target ami
```

### Update Terragrunt

For DC03 (uses 2016):

```hcl
inputs = {
  windows_os         = "Windows_Server"
  windows_os_version = "2016-English-Full-Base"

  additional_windows_ami_filters = [
    {
      name   = "image-id"
      values = ["ami-xxxxxxxxxxxx"]  # Your goad-dc-base-2016 AMI ID
    }
  ]

  windows_ami_owners = ["self"]
}
```

## Tags

The AMI is tagged with:

- `Name`: goad-dc-base-2016
- `Lab`: GOAD
- `Role`: DomainController
- `ManagedBy`: warpgate
- `BaseOS`: WindowsServer2016
