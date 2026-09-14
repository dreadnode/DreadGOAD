# Add a new lab

To create a new lab, create a folder in `ad/` with the lab name. The `dreadgoad` CLI automatically discovers labs by scanning the `ad/` directory (use `dreadgoad lab list` to verify your lab is detected).

## Directory structure

Create the following structure inside `ad/<lab_name>/`:

```text
ad/<lab_name>/
    range.yml                       # range kind, creation policy, lifecycle hooks
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

### Environment overlays

Rather than maintaining full config copies per environment, create small
overlay files (`{env}-overlay.json`) that contain only the fields that
differ from the base `config.json`. The CLI merges them at runtime using
RFC 7386 JSON Merge Patch. See `docs/cli.md` in the repository for the overlay format and resolution order.

Arrays replace, they do not merge. If an overlay redeclares a host's `vulns`,
that list becomes the complete set for the environment and the base list is
discarded, so adding a vuln to `config.json` alone does nothing wherever an
overlay names that host. The failure is silent in both directions: nothing
dangles, so a referential check sees a valid document, and `vulnerabilities.yml`
just never includes the missing role, leaving `PLAY RECAP` reporting `failed=0`.

After adding a vuln to any `config.json`, add it to every `{env}-overlay.json`
that redeclares that host. `TestLabConfigIntegrity` enforces this via
`CheckOverlayDrops`; a deliberate removal goes in
`cli/internal/labconfig/testdata/known_findings.txt` with a reason.

## Inventory files

The `data/inventory` file is an Ansible inventory that defines host groups and connection variables (WinRM settings, credentials). Each provider also has its own `inventory` file under `providers/<provider>/` that overrides connection-specific values (IP addresses, ports) for that provider.

The `data/inventory_disable_vagrant` inventory is used by the `disable_vagrant.yml` and `enable_vagrant.yml` playbooks to manage the vagrant user on VMs.

## Provider-specific files

- **Terraform/Terragrunt providers** (aws, azure, proxmox): Include `windows.tf` and optionally `linux.tf` files that define the VMs as Terraform resources. Infrastructure modules live in `infra/` and are invoked via Terragrunt.
- **Vagrant providers** (virtualbox, vmware): Include a `Vagrantfile` that defines VM resources, networking, and linked clones.
- **Ludus provider**: Uses a `config.yml` that describes the VMs in Ludus format.

## Lab discovery

The CLI discovers labs automatically by scanning the `ad/` directory. No
additional central registration step is needed. The lab name is derived from the
directory name.

To make a range available in the web console's **Create a new environment**
workflow, declare the provider scaffolding contract in `range.yml`:

```yaml
schema_version: 1
display_name: Example Range
kind: service-range                 # or active-directory
variants:
  supported: false
inspection:
  profile: example-range             # registered CLI health/validation profile
infrastructure:
  azure:
    deployment: example-deployment
    scaffold_profile: template      # complete authored environment template
    template_environment: example-dev
    default_region: centralus
    network:
      cidr: 10.60.0.0/16
      editable: false
lifecycle:
  session_init: []
```

The provider must also have `providers/<provider>/inventory`, and the referenced
template must exist below `infra/`. For the legacy `active-directory` scaffold
profile, every host in `data/config.json` must have a corresponding Terragrunt
host module in the reference environment. `dreadgoad lab list --json` only
advertises provider combinations that pass these structural checks, preventing
the console from offering a range it can only partially create.

`inspection.profile` names an implementation registered by the CLI; manifests
cannot provide executable paths. Active Directory ranges may omit it and use
the legacy AD inspector. Service ranges must select a profile so health and
validation never silently fall back to AD-specific checks.

`template` profiles currently require a fixed private IPv4 `/16` and must not
set `network.editable: true`. The template owns its subnet layout, so accepting
an arbitrary CIDR without a range-specific renderer would make the config and
the copied infrastructure disagree.

Run `dreadgoad lab list --json` to confirm the range appears and inspect its
`provider_settings`. Then exercise `dreadgoad --config <path> --env <name> env
create <name>` in a disposable checkout before exposing it to console users.
