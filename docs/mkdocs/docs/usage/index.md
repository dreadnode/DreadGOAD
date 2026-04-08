# Usage

DreadGOAD is managed through the `dreadgoad` CLI. All operations are available as commands and subcommands.

```bash
dreadgoad --help
```

See the [CLI Reference](../cli-reference.md) for the full command listing.

## Common workflows

### First-time setup

```bash
dreadgoad config init          # Create default config
dreadgoad doctor               # Verify dependencies
dreadgoad env create dev       # Create an environment
```

### Deploy a lab

```bash
dreadgoad infra init           # Initialize Terragrunt
dreadgoad infra apply          # Provision infrastructure
dreadgoad inventory sync       # Sync inventory with AWS
dreadgoad provision            # Run Ansible provisioning
dreadgoad validate             # Validate vulnerabilities
```

### Day-to-day operations

```bash
dreadgoad lab status           # Check lab state
dreadgoad lab stop             # Stop all instances
dreadgoad lab start            # Start all instances
dreadgoad health-check         # Verify lab health
```
