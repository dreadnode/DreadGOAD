# CLI Reference

Complete reference for the `dreadgoad` command-line interface.

DreadGOAD orchestrates the deployment and management of intentionally
vulnerable Active Directory environments for security research and testing.

## Global Flags

| Flag | Description |
|------|-------------|
| `--config string` | Config file path |
| `--debug` | Enable debug/verbose output |
| `-e, --env string` | Target environment: dev, staging, prod (default `"staging"`) |
| `--region string` | AWS region (default: from inventory) |
| `-v, --version` | Print version information |

---

## Getting Started

### init

Interactive setup wizard. Walks through provider selection and connectivity probing, then writes a working `dreadgoad.yaml`.

```bash
dreadgoad init
```

For Ludus the wizard prefers an `~/.ssh/config` Host alias so existing key/agent setups (including 1Password) carry through with zero extra configuration.

### up

Deploy the lab end-to-end: doctor → infra apply → provision → health-check. Stops on the first failing step and prints a resume hint.

```bash
dreadgoad up                       # full pipeline
dreadgoad up --from provision      # resume from a step
dreadgoad up --skip-doctor         # bypass pre-flight checks
dreadgoad up --limit dc01          # narrow provisioning to one host
```

| Flag | Description |
|------|-------------|
| `--from string` | Resume from this step (`doctor`, `infra`, `provision`, `health-check`) |
| `--skip-doctor` | Skip the doctor pre-flight checks |
| `--limit string` | Limit provisioning to specific hosts |
| `--plays string` | Comma-separated playbooks to run (default: all) |
| `--max-retries int` | Max retry attempts for provisioning |
| `--retry-delay int` | Delay between retries in seconds |
| `--module string` | Target a specific infra module |
| `--exclude string` | Exclude infra modules (comma-separated) |

### config

Manage CLI configuration.

#### `config init`

Create default configuration file.

```bash
dreadgoad config init
```

#### `config show`

Display current effective configuration.

```bash
dreadgoad config show
```

#### `config set`

Set a configuration value.

```bash
dreadgoad config set <key> <value>
```

### doctor

Run pre-flight system checks.

Verifies that all required tools and configurations are in place:

- ansible-core version
- AWS CLI
- Python
- Ansible collections
- Credentials
- Inventory

```bash
dreadgoad doctor
```

### env

Manage deployment environments.

#### `env create`

Create a new deployment environment.

```bash
dreadgoad env create <env-name>
```

#### `env list`

List available environments.

```bash
dreadgoad env list
```

---

## Infrastructure

### infra

Manage DreadGOAD infrastructure via Terragrunt. Operates on the `infra/` directory. By default, commands operate on all modules (`run-all`). Use `--module` to target a specific module.

| Flag | Description |
|------|-------------|
| `-d, --deployment string` | Deployment name |

#### `infra init`

Initialize Terragrunt modules.

```bash
dreadgoad infra init
```

#### `infra plan`

Plan infrastructure changes.

```bash
dreadgoad infra plan
```

#### `infra apply`

Apply infrastructure changes.

```bash
dreadgoad infra apply
```

#### `infra destroy`

Destroy infrastructure.

```bash
dreadgoad infra destroy
```

#### `infra output`

Show Terragrunt outputs (JSON).

```bash
dreadgoad infra output
```

#### `infra validate`

Validate environment configuration.

```bash
dreadgoad infra validate
```

### ami

AMI image management.

#### `ami build`

Build an AMI from a warpgate template.

```bash
dreadgoad ami build [template]
```

#### `ami list`

List AMIs built by warpgate.

```bash
dreadgoad ami list [--filter-name <build-name>]
```

#### `ami delete`

Deregister AMIs and delete associated EBS snapshots.

```bash
dreadgoad ami delete <ami-id> [ami-id...] [--delete-snapshots=false] [--yes]
```

#### `ami list-resources`

List Image Builder pipeline resources created by warpgate.

```bash
dreadgoad ami list-resources
```

#### `ami clean-resources`

Remove Image Builder pipeline resources (not AMIs).

```bash
dreadgoad ami clean-resources [template]
```

---

## Provisioning

### provision

Run GOAD provisioning playbooks with retry logic.

Runs Ansible playbooks to provision Active Directory infrastructure with error-specific retry strategies, SSM session management, and idle timeout monitoring.

| Flag | Description |
|------|-------------|
| `--from string` | Resume provisioning from this playbook onward |
| `--limit string` | Limit execution to specific hosts |
| `--max-retries int` | Max retry attempts |
| `--plays string` | Comma-separated playbooks to run (default: all) |
| `--retry-delay int` | Delay between retries in seconds |

```bash
# Run all provisioning playbooks
dreadgoad provision

# Resume from a specific playbook
dreadgoad provision --from vulnerabilities.yml

# Run specific playbooks only
dreadgoad provision --plays "ad-groups.yml,ad-acl.yml"

# Limit to specific hosts with retries
dreadgoad provision --limit dc01 --max-retries 5
```

### ad-users

Ensure AD users exist (runs `ad-data.yml`).

Shortcut for `provision --plays ad-data.yml`.

| Flag | Description |
|------|-------------|
| `--limit string` | Limit execution to specific hosts |
| `--max-retries int` | Max retry attempts |
| `--plays string` | Comma-separated playbooks to run |
| `--retry-delay int` | Delay between retries in seconds |

```bash
dreadgoad ad-users
```

### variant

Generate GOAD variants with randomized entity names. Creates a graph-isomorphic copy of GOAD with randomized names while preserving structure, relationships, vulnerabilities, and attack paths.

#### `variant generate`

Generate a new GOAD variant.

```bash
dreadgoad variant generate
```

---

## Lab Operations

### lab

Manage DreadGOAD lab lifecycle.

#### `lab status`

Show lab instance states.

```bash
dreadgoad lab status
```

#### `lab start`

Start stopped lab instances.

```bash
dreadgoad lab start
```

#### `lab stop`

Stop running lab instances.

```bash
dreadgoad lab stop
```

#### `lab start-vm`

Start a specific lab VM.

```bash
dreadgoad lab start-vm <hostname>
```

#### `lab stop-vm`

Stop a specific lab VM.

```bash
dreadgoad lab stop-vm <hostname>
```

#### `lab restart-vm`

Restart a specific lab VM.

```bash
dreadgoad lab restart-vm <hostname>
```

#### `lab destroy-vm`

Terminate a specific lab VM.

```bash
dreadgoad lab destroy-vm <hostname>
```

#### `lab list`

List available DreadGOAD labs and their providers.

```bash
dreadgoad lab list
```

### ssm

Manage AWS SSM sessions.

#### `ssm status`

Show active SSM sessions.

```bash
dreadgoad ssm status
```

#### `ssm cleanup`

Terminate stale SSM sessions.

```bash
dreadgoad ssm cleanup
```

#### `ssm connect`

Start interactive SSM session.

```bash
dreadgoad ssm connect <host>
```

#### `ssm run`

Run PowerShell commands across instances via SSM.

```bash
dreadgoad ssm run
```

### health-check

Verify all lab instances are healthy.

Runs health checks across all lab instances via SSM to verify:

- Domain controllers responding
- AD replication
- Domain trusts
- DNS resolution
- Member server connectivity
- Critical services (IIS, MSSQL)

```bash
dreadgoad health-check
```

### diagnose

Run diagnostic checks against domain controllers.

Runs the `diagnose-dc01` playbook from an independent host to verify network connectivity, LDAP, WinRM, and DNS for the primary domain controller.

| Flag | Description |
|------|-------------|
| `--dc01-ip string` | Override dc01 IP address (skips AWS lookup) |

```bash
# Auto-detect dc01 IP from AWS
dreadgoad diagnose

# Specify dc01 IP manually
dreadgoad diagnose --dc01-ip 10.0.10.10
```

### verify-trusts

Verify domain trust relationships.

Validates parent-child trusts, forest trusts, and cross-domain authentication.

```bash
dreadgoad verify-trusts
```

---

## Validation

### validate

Validate GOAD vulnerability configurations.

Checks credentials, Kerberos, SMB, delegation, MSSQL, ADCS, ACLs, trusts, SID filtering, scheduled tasks, LLMNR/NBT-NS, GPO abuse, gMSA, LAPS, and services.

| Flag | Description |
|------|-------------|
| `--format string` | Output format: `table` or `json` (default `"table"`) |
| `--no-fail` | Don't exit with error on failed checks |
| `--output string` | JSON report output path |
| `--quick` | Quick validation of critical vulnerabilities only |
| `--verbose` | Enable verbose output |

```bash
# Full validation with table output
dreadgoad validate

# Quick check of critical vulnerabilities
dreadgoad validate --quick

# Export JSON report
dreadgoad validate --format json --output report.json

# Verbose output, don't fail on errors
dreadgoad validate --verbose --no-fail
```

---

## Extensions

### extension

Manage lab extensions. List, inspect, and provision lab extensions such as ELK, Exchange, Guacamole, and more.

Alias: `ext`

#### `extension list`

List available extensions.

```bash
dreadgoad extension list
```

#### `extension provision`

Provision a specific extension.

```bash
dreadgoad extension provision <name>
```

#### `extension provision-all`

Provision all enabled extensions for the active environment.

```bash
dreadgoad extension provision-all
```

---

## Inventory

### inventory

Manage Ansible inventory.

#### `inventory sync`

Synchronize inventory with AWS instance IDs.

```bash
dreadgoad inventory sync
```

#### `inventory show`

Display current inventory.

```bash
dreadgoad inventory show
```

#### `inventory mapping`

Generate instance-to-IP mapping for Ansible optimization.

```bash
dreadgoad inventory mapping
```

---

## Shell Completion

### completion

Generate shell autocompletion scripts.

```bash
# Bash
dreadgoad completion bash

# Zsh
dreadgoad completion zsh

# Fish
dreadgoad completion fish

# PowerShell
dreadgoad completion powershell
```

To load completions in your current shell session:

```bash
# Bash
source <(dreadgoad completion bash)

# Zsh
source <(dreadgoad completion zsh)
```
