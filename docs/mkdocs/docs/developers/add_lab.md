# Add a new lab

To create a new lab, create a folder in `ad/` with the lab name. The `dreadgoad` CLI automatically discovers labs by scanning the `ad/` directory (use `dreadgoad lab list` to verify your lab is detected).

## Directory structure

Create the following structure inside `ad/<lab_name>/`:

```text
ad/<lab_name>/
    data/
        config.json                 # JSON containing all the lab information
        inventory                   # global lab inventory file with the VM groups and the main variables
        inventory_disable_vagrant   # inventory to disable/enable vagrant user
    files/                          # extra files needed during provisioning (scripts, templates, etc.)
    providers/
        aws|azure|proxmox/          # Terraform/Terragrunt based providers
            inventory               # inventory specific to the provider
            linux.tf                # linux VMs
            windows.tf              # windows VMs
        ludus/                      # Ludus provider
            inventory               # inventory specific to the provider
            config.yml              # Ludus configuration file
        virtualbox|vmware/          # Vagrant based providers
            inventory               # inventory specific to the provider
            Vagrantfile             # VM definitions
    scripts/                        # PowerShell or other scripts used by Ansible roles
```

## config.json format

The `config.json` file in `data/` defines all lab hosts, their domains, users, groups, vulnerabilities, and security settings. Required top-level structure:

```json
{
  "lab": {
    "hosts": {
      "<host_id>": {
        "hostname": "short hostname",
        "type": "dc|server",
        "local_admin_password": "strong password",
        "domain": "example.local",
        "path": "DC=example,DC=local",
        "local_groups": {
          "Administrators": ["domain\\user"],
          "Remote Desktop Users": ["domain\\group"]
        },
        "scripts": ["script.ps1"],
        "vulns": ["vuln_name"],
        "security": ["security_feature"],
        "security_vars": {}
      }
    }
  }
}
```

Key fields per host:

| Field | Required | Description |
|-------|----------|-------------|
| `hostname` | Yes | Short hostname for the VM |
| `type` | Yes | `dc` for domain controller, `server` for member server |
| `domain` | Yes | FQDN of the AD domain this host belongs to |
| `path` | Yes | LDAP distinguished name path |
| `local_admin_password` | Yes | Local administrator password |
| `local_groups` | No | Local group memberships |
| `scripts` | No | PowerShell scripts to execute on the host |
| `vulns` | No | Vulnerability configurations to apply |
| `security` | No | Security hardening features to enable |

See `ad/GOAD/data/config.json` for a complete reference example.

## Inventory files

The `data/inventory` file is an Ansible inventory that defines host groups and connection variables (WinRM settings, credentials). Each provider also has its own `inventory` file under `providers/<provider>/` that overrides connection-specific values (IP addresses, ports) for that provider.

The `data/inventory_disable_vagrant` inventory is used by the `disable_vagrant.yml` and `enable_vagrant.yml` playbooks to manage the vagrant user on VMs.

## Provider-specific files

- **Terraform/Terragrunt providers** (aws, azure, proxmox): Include `windows.tf` and optionally `linux.tf` files that define the VMs as Terraform resources. Infrastructure modules live in `infra/` and are invoked via Terragrunt.
- **Vagrant providers** (virtualbox, vmware): Include a `Vagrantfile` that defines VM resources, networking, and linked clones.
- **Ludus provider**: Uses a `config.yml` that describes the VMs in Ludus format.

## Lab discovery

The CLI discovers labs automatically by scanning the `ad/` directory. No additional registration step is needed. Run `dreadgoad lab list` to confirm your lab appears. The lab name is derived from the directory name.
