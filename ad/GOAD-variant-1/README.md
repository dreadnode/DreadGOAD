# GOAD VARIANT-1

This is a graph-isomorphic variant of the GOAD (Game of Active Directory) lab environment.

## About This Variant

- **All entity names have been randomized** while preserving the complete structure
- **Attack paths remain identical** to the original GOAD
- **All vulnerabilities preserved** with the same relationships
- **All 6 provider configs included**: VirtualBox, VMware, Proxmox, AWS, Azure, Ludus

## Structure

- **3 domains** with parent-child and trust relationships
- **5 VMs**: 3 Domain Controllers, 2 Servers
- **40+ users** with randomized names
- **18+ groups**, **8 OUs**, **20+ ACLs**

## Usage

Deploy exactly like the original GOAD:

```bash
# Navigate to provider directory
cd providers/virtualbox  # or vmware, proxmox, aws, azure, ludus

# Follow provider-specific setup instructions
# Provisioning works identically to GOAD
```

## Mapping Reference

See `mapping.json` for the complete entity mapping from GOAD to this variant.

## Purpose

This variant allows:
- **Multiple lab instances** without name conflicts
- **Training scenarios** with fresh environments
- **Testing automation** against different entity names
- **Validation** that tools work beyond hardcoded names

