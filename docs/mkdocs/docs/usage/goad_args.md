# CLI Commands

The `dreadgoad` CLI uses a structured command and subcommand pattern. Every operation is a top-level command or a subcommand grouped under a resource.

## Command structure

```text
dreadgoad <command> [subcommand] [flags]
```

## Global flags

These flags apply to any command:

| Flag | Description |
|---|---|
| `--env <name>` | Select the environment to operate on |
| `--config <path>` | Path to a config file (default: `dreadgoad.yaml`) |
| `--debug` | Enable debug logging |
| `--region <region>` | AWS region override |

## Usage examples

### Provisioning

Run the full provisioning playbook sequence:

```bash
dreadgoad provision
```

Start provisioning from a specific playbook:

```bash
dreadgoad provision --from ad-trusts.yml
```

Run specific playbooks with a host limit:

```bash
dreadgoad provision --plays ad-data.yml --limit dc01
```

### Lab lifecycle

```bash
dreadgoad lab status
dreadgoad lab start
dreadgoad lab stop
dreadgoad lab start-vm dc01
dreadgoad lab stop-vm dc01
dreadgoad lab restart-vm dc01
dreadgoad lab destroy-vm dc01
dreadgoad lab list
```

### Infrastructure

```bash
dreadgoad infra init
dreadgoad infra plan
dreadgoad infra plan --module network
dreadgoad infra apply
dreadgoad infra destroy
dreadgoad infra output
dreadgoad infra validate
```

### Validation and diagnostics

```bash
dreadgoad validate
dreadgoad validate --format json --output results.json
dreadgoad health-check
dreadgoad diagnose
dreadgoad doctor
dreadgoad verify-trusts
```

### Environment and configuration

```bash
dreadgoad env create dev
dreadgoad env list
dreadgoad config init
dreadgoad config show
dreadgoad config set key value
```

### Extensions

```bash
dreadgoad extension list
dreadgoad extension provision <ext>
dreadgoad extension provision-all
```

### SSM sessions

```bash
dreadgoad ssm status
dreadgoad ssm connect <target>
dreadgoad ssm run <target> <command>
dreadgoad ssm cleanup
```

### Inventory and AMI

```bash
dreadgoad inventory sync
dreadgoad inventory show
dreadgoad inventory mapping
dreadgoad ami build
dreadgoad ami list
dreadgoad ami delete
dreadgoad ami list-resources
dreadgoad ami clean-resources
```

See the [CLI Reference](../cli-reference.md) for the full command listing and detailed flag descriptions.
