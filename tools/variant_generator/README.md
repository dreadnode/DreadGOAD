# GOAD Variant Generator

Tools for creating graph-isomorphic variants of GOAD (Game of Active Directory) lab environments with randomized entity names while preserving complete structure, relationships, vulnerabilities, and attack paths.

## Components

### `name_generator.py`
Generates realistic, human-readable names for all entities:
- **Users**: Realistic first/last name combinations (96 first names × 96 last names)
- **Hosts**: Tech-themed server names (30 prefixes × 20 suffixes)
- **Domains**: Corporate-style domain names
- **Groups**: Capitalized theme-based group names
- **OUs**: Region/division-style organizational unit names
- **gMSA Accounts**: Animal-themed service accounts (e.g., `gmsaPhoenix`)
- **Passwords**: Complexity-equivalent random passwords using cryptographic randomness

### `goad_variant_generator.py`
Main generator that orchestrates variant creation:
- Extracts entities from source GOAD configuration
- Generates comprehensive entity mappings
- Performs ordered text replacements (longest-first to avoid substring collisions)
- Transforms 97+ files across multiple formats (JSON, YAML, PS1, Terraform, Vagrantfile)
- Rebuilds complex identifiers (DN paths, SPNs, ACL keys)
- Validates completeness and structural integrity

## Usage

### Basic Usage

From the project root:

```bash
python3 tools/variant_generator/goad_variant_generator.py \
  --source ad/GOAD \
  --target ad/GOAD-variant-2 \
  --name variant-2
```

### As a Python Module

```python
from tools.variant_generator import GOADVariantGenerator

generator = GOADVariantGenerator(
    source_path='ad/GOAD',
    target_path='ad/GOAD-variant-2',
    variant_name='variant-2'
)

# Generate mappings
generator.generate_mappings()

# Copy and transform all files
generator.copy_and_transform()

# Validate result
generator.validate()
```

### Command Line Options

```
--source PATH       Source GOAD directory (default: ad/GOAD)
--target PATH       Target variant directory (default: ad/GOAD-variant-1)
--name NAME         Variant name (default: variant-1)
```

## What Gets Transformed

### Entities
- **3 domains** with parent-child and trust relationships
- **5 VMs** (3 domain controllers, 2 servers)
- **40+ users** with realistic first.last usernames
- **18+ groups** (domain local, global, universal)
- **8 OUs** (organizational units)
- **20+ ACLs** with reconstructed keys
- **Service accounts** (sql_svc, gmsa accounts)
- **Passwords** (regenerated with equivalent complexity)

### Files and Formats
- **JSON**: config.json and data files
- **YAML/YML**: inventory files, Ludus configs
- **PowerShell (.ps1)**: 13+ automation scripts
- **Terraform (.tf)**: Cloud provider configurations
- **Vagrantfile**: VM provisioning configs
- **Text files**: User data, documentation
- **Shell scripts**: Automation utilities

### All 6 Providers
- VirtualBox
- VMware (Desktop and ESXi)
- Proxmox
- AWS
- Azure
- Ludus

## Output Files

The generator produces:

1. **Complete variant directory** (`ad/GOAD-variant-X/`)
   - Identical structure to source GOAD
   - All files transformed with new names
   - Binary files copied as-is

2. **mapping.json** - Entity mapping reference
   ```json
   {
     "domains": {...},
     "netbios": {...},
     "hosts": {...},
     "users": {...},
     "passwords": {...},
     "groups": {...},
     "ous": {...},
     "misc": {...}
   }
   ```

3. **README.md** - Variant documentation

## Ordered Replacement Strategy

To avoid substring collisions, replacements are applied in this order:

1. FQDNs (longest): `castelblack.north.sevenkingdoms.local`
2. Domain-qualified usernames: `NORTH\\jon.snow`
3. DN paths: `DC=north,DC=sevenkingdoms,DC=local`
4. SPNs: `MSSQLSvc/castelblack.north.sevenkingdoms.local:1433`
5. Computer accounts: `castelblack$`
6. Sub-domains: `north.sevenkingdoms.local` (before parent)
7. Parent domains: `sevenkingdoms.local`
8. Usernames: `jon.snow`
9. Groups: `Stark`
10. OUs: `Westerlands`

This ensures `north.sevenkingdoms.local` is replaced before `sevenkingdoms.local`.

