# Ludus

!!! success "Thanks!"
    Huge shoutout to @badsectorlabs for Ludus and Erik for his support and tests during the Ludus provider creation

<div align="center">
  <img alt="ludus" width="200" height="150" src="./../img/icon_ludus.png">
  <img alt="icon_ansible" width="145"  height="150" src="./../img/icon_ansible.png">
</div>

!!! info "Local or remote operation"
    DreadGOAD with the Ludus provider runs in one of two modes. In **local mode** (default) DreadGOAD is installed on the Ludus server itself and shells out to the local `ludus` CLI. In **SSH mode** DreadGOAD runs on a workstation and reaches the Ludus server over SSH (the `ludus` CLI is invoked on the remote host) — see [SSH-mode configuration](#ssh-mode-configuration).

## Prerequisites

- A working Ludus v2 installation: [https://docs.ludus.cloud/docs/quick-start/install-ludus/](https://docs.ludus.cloud/docs/quick-start/install-ludus/)
- An **admin** user created with an API key
- `zip` package installed on the server (`apt-get install -y zip`)
- Packer templates built in Ludus for the required VM images (see [Building Packer Templates](#building-packer-templates) below)

## Building Packer Templates

Before deploying the GOAD lab, Ludus needs VM templates available in its catalog. Unlike the Proxmox provider (which uses Packer + Terraform directly), Ludus manages templates through its own CLI.

### Required Templates

The GOAD lab config references these templates:

| Template Name | Used By |
|---|---|
| `win2019-server-x64-template` | DC01, DC02, SRV02, SRV03 |
| `win2016-server-x64-template` | DC03 |
| `debian-11-x64-server-template` | Router |

### Adding and Building Templates

Use the Ludus CLI to add templates from the Ludus template catalog and build them:

```bash
# List available templates in the Ludus catalog
ludus templates list

# Add Windows Server 2019 template
ludus templates add -n win2019-server-x64-template

# Add Windows Server 2016 template
ludus templates add -n win2016-server-x64-template

# Add Debian 11 router template
ludus templates add -n debian-11-x64-server-template

# Build all added templates (this takes a while, especially Windows)
ludus templates build

# Check build status
ludus templates status
```

!!! info "Template build times"
    Windows template builds can take 30-60+ minutes each depending on hardware. The Debian template is typically much faster. You can monitor progress with `ludus templates status`.

!!! tip "Custom templates"
    If you need a specific Windows version or configuration, see the [Ludus template documentation](https://docs.ludus.cloud/docs/templates) for creating custom templates.

## DreadGOAD Configuration

Initialize the configuration file:

```bash
dreadgoad config init
```

Configure Ludus-specific settings in `dreadgoad.yaml`:

```yaml
provider: ludus
ludus:
  api_key: YOUR_ADMIN_API_KEY
  use_impersonation: true
```

You can also set the API key via environment variable instead:

```bash
export LUDUS_API_KEY='YOUR_ADMIN_API_KEY'
```

!!! info
    On Ludus, the IP range is assigned dynamically by the Ludus range system and is not user-configurable. The CLI automatically detects the assigned range during provisioning.

### SSH-mode configuration

If DreadGOAD is *not* installed on the Ludus server itself, configure SSH so the CLI can shell out to a remote `ludus` binary on the server. The presence of `ludus.ssh_host` is the toggle: leave it empty for local mode, set it for SSH mode.

```yaml
provider: ludus
ludus:
  api_key: YOUR_ADMIN_API_KEY
  use_impersonation: true
  ssh_host: ludus.example.com   # set to enable SSH mode
  ssh_user: root                # default: root
  ssh_port: 22                  # default: 22
  ssh_key_path: ~/.ssh/id_ed25519  # private key for key-based auth
  # ssh_password: hunter2       # password auth (uses sshpass — pick one or the other)
```

| Field | Required | Notes |
|---|---|---|
| `ssh_host` | yes (for SSH mode) | Hostname or IP of the Ludus server |
| `ssh_user` | no | SSH login (default `root`) |
| `ssh_port` | no | TCP port (default `22`) |
| `ssh_key_path` | one of key/password | Path to a private key |
| `ssh_password` | one of key/password | Password — requires `sshpass` on PATH |

When `ssh_password` is set, `sshpass` must be installed locally (`apt install sshpass` / `brew install hudochenkov/sshpass/sshpass`). `dreadgoad doctor` validates the SSH endpoint is reachable and that the right helper binaries are present.

## Installation

```bash
# Check prerequisites
dreadgoad --provider ludus doctor

# Deploy VMs via Ludus range
dreadgoad --provider ludus infra apply

# Provision the AD lab
dreadgoad --provider ludus provision
```

### What happens during `infra apply`

1. DreadGOAD creates a Ludus user for the lab (if impersonation is enabled)
2. The Ludus range configuration is generated from the GOAD lab config (`ad/GOAD/providers/ludus/config.yml`)
3. The config is pushed via `ludus range config set`
4. VMs are deployed using `ludus range deploy`
5. DreadGOAD polls `ludus range status` every 30 seconds until all VMs report SUCCESS
6. Ludus handles sysprep, hostname configuration, and initial Windows setup
7. A router VM (`debian-11-x64-server-template`) is deployed for network routing

### What happens during `provision`

!!! note "Transport: WinRM"
    Unlike the AWS provider (which uses SSM), Ludus provisioning runs Ansible over **WinRM** to the Windows VMs on the Ludus VLAN. All target VMs must be **powered on** for provisioning, `health-check`, and `validate` to succeed — `infra apply` leaves them running, but if you stop the lab between steps, run `dreadgoad --provider ludus lab start` first.

1. The inventory file is automatically bootstrapped from the Ludus provider template
2. IP addresses are resolved from the deployed Ludus range (format: `10.{range_number}.10.x`)
3. Ansible playbooks run over WinRM to configure:
    - Active Directory domains and domain controllers
    - Forest and domain trusts
    - Users, groups, OUs, and GPOs
    - MSSQL Server Express instances
    - ADCS (Active Directory Certificate Services)
    - IIS web servers
    - Vulnerable configurations (Kerberoasting, ASREPRoast, delegation, ACL abuse, etc.)

## VM Configuration

The default GOAD lab deploys 6 VMs:

| VM | Role | Domain | IP (last octet) | Template |
|---|---|---|---|---|
| DC01 | Domain Controller | sevenkingdoms.local | .10 | win2019-server-x64-template |
| DC02 | Child Domain Controller | north.sevenkingdoms.local | .11 | win2019-server-x64-template |
| DC03 | Domain Controller | essos.local | .12 | win2016-server-x64-template |
| SRV02 | Member Server (MSSQL, IIS) | north.sevenkingdoms.local | .22 | win2019-server-x64-template |
| SRV03 | Member Server (MSSQL, ADCS, IIS) | essos.local | .23 | win2019-server-x64-template |
| Router | Network Router | - | .254 | debian-11-x64-server-template |

All Windows VMs use 2 vCPUs and 4 GB RAM.

## Monitoring Deployment

Check the Ludus range status at any time:

```bash
LUDUS_API_KEY='YOUR_KEY' ludus range status
```

Or use the DreadGOAD CLI:

```bash
dreadgoad --provider ludus lab status
```

## Verification

`health-check` and `validate` connect to the Windows VMs over WinRM and run ad-hoc Ansible commands, so the VMs must be running. If they were stopped via `lab stop`, start them again with `dreadgoad --provider ludus lab start` (or `ludus range start`) before verifying.

After provisioning completes, verify the lab is working correctly:

```bash
# Full health check (AD controllers, replication, DNS, trusts, services)
dreadgoad --provider ludus health-check

# Verify AD forest trusts specifically
dreadgoad --provider ludus verify-trusts

# Run the full vulnerability validation suite
dreadgoad --provider ludus validate
```

### Expected health-check output

A healthy GOAD lab should report 22 passed checks:

```text
CHECK                                    STATUS     DETAIL
------------------------------------------------------------------------------------------
DC01 AD Domain Controller                OK         KINGSLANDING
DC01 AD Replication                      OK         no replication failures
DC02 AD Domain Controller                OK         WINTERFELL
DC02 AD Replication                      OK         no replication failures
DC03 AD Domain Controller                OK         MEEREEN
DC03 AD Replication                      OK         no replication failures
DC03 Trusts (sevenkingdoms.local)        OK         sevenkingdoms.local
DC01 Trusts (essos.local)                OK         essos.local
...
Results: 22 passed, 0 failed
```

## Start / Stop / Status

You can manage the lab lifecycle:

```bash
# Check lab status
dreadgoad --provider ludus lab status

# Stop all VMs
dreadgoad --provider ludus lab stop

# Start all VMs
dreadgoad --provider ludus lab start
```

## Troubleshooting

### Template not found during `infra apply`

If Ludus reports a template is not available, verify your templates are built:

```bash
ludus templates list
ludus templates status
```

If a template is missing, add and build it (see [Building Packer Templates](#building-packer-templates)).

### WinRM timeouts during provisioning

Some playbooks (especially MSSQL installation and Windows Updates) can take a long time. Increase the idle timeout in `dreadgoad.yaml`:

```yaml
idle_timeout: 3600
```

### Resuming failed provisioning

If provisioning fails partway through, you can resume from a specific playbook:

```bash
dreadgoad --provider ludus provision --from ad-servers.yml
```

The playbooks run in order. Common resume points:

| Playbook | What it does |
|---|---|
| `build.yml` | Domain controller promotion, DNS, domain joins |
| `ad-servers.yml` | IIS, MSSQL, ADCS installation |
| `ad-parent_domain.yml` | Parent domain (sevenkingdoms.local) users/groups |
| `ad-child_domain.yml` | Child domain (north.sevenkingdoms.local) config |
| `ad-members.yml` | Domain membership and delegation |
| `ad-trusts.yml` | Forest trusts between domains |
| `ad-data.yml` | Vulnerable configurations and data seeding |
| `ad-gmsa.yml` | Group Managed Service Accounts |
| `ad-laps.yml` | LAPS deployment |
| `ad-relations.yml` | ACL-based attack paths |
| `adcs.yml` | Certificate Services misconfigurations |
| `ad-acl.yml` | ACL abuse scenarios |
| `servers.yml` | Final server hardening and MSSQL config |
| `security.yml` | Defender and firewall settings |
| `vulnerabilities.yml` | Additional vulnerability configurations |

### VM stuck or unresponsive

If a VM becomes unresponsive (e.g. stuck during a reboot), you can hard-reset it via Proxmox:

```bash
# Find the Proxmox VMID from ludus range status
ludus range status

# Hard reset the VM via qm
qm reset <VMID>
```

### ADCS template zip missing

The ADCS role requires a `cert_templates.zip` file. If provisioning fails at the ADCS stage with a missing zip error:

```bash
# Ensure zip is installed
apt-get install -y zip

# Create the zip from the ADCS template files
cd /opt/DreadGOAD/ad/GOAD/providers/ludus
# The provision command handles this automatically, but if needed manually:
zip -r cert_templates.zip cert_templates/
```

### "No running instances found"

If health-check or verify-trusts reports no instances, make sure the VMs are powered on:

```bash
ludus range status
# or
dreadgoad --provider ludus lab start
```

## How It Works

Unlike the Proxmox provider (which uses Packer + Terraform), the Ludus provider delegates VM lifecycle management entirely to the Ludus platform:

1. **Infrastructure**: `ludus range config set` + `ludus range deploy` (no Terraform)
2. **Networking**: Ludus assigns a dedicated VLAN and IP range per user/range
3. **Provisioning**: Ansible playbooks over WinRM (same as all other providers)
4. **Command execution**: Ansible ad-hoc commands via `win_shell` module for health checks and validation

The DreadGOAD CLI wraps these operations into a consistent interface, so `dreadgoad infra apply`, `dreadgoad provision`, and `dreadgoad health-check` work the same regardless of provider.
