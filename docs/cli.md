# DreadGOAD CLI Configuration

The `dreadgoad` CLI uses [Viper](https://github.com/spf13/viper) for
configuration, with values resolved in this priority order:

1. CLI flags (`--env`, `--region`, `--debug`)
2. Environment variables (`DREADGOAD_ENV`, `DREADGOAD_REGION`, etc.)
3. Config file (YAML)
4. Built-in defaults

## Config File

The config file is **optional**. When present it is loaded from:

1. Path given via `--config` flag
2. `~/.config/dreadgoad/dreadgoad.yaml`
3. `./dreadgoad.yaml` (current directory)

### Creating a Config File

```bash
dreadgoad config init
```

This writes a default config to `~/.config/dreadgoad/dreadgoad.yaml`.

### Viewing Effective Config

```bash
dreadgoad config show
```

### Setting a Value

```bash
dreadgoad config set env staging
dreadgoad config set environments.dev.variant true
```

## Reference

```yaml
# Active environment (selects into the environments map below)
env: staging

# AWS region override (default: resolved from inventory)
# region: us-west-2

debug: false
max_retries: 3      # Ansible playbook retry attempts
retry_delay: 30     # Seconds between retries
idle_timeout: 1200  # Seconds before killing idle ansible-playbook

# Auto-detected by walking up from cwd looking for ansible/ directory
# project_root: /path/to/DreadGOAD

# Log directory (default: ~/.ansible/logs/goad)
# log_dir: ~/.ansible/logs/goad

# Per-environment settings
environments:
  dev:
    variant: true
    variant_source: ad/GOAD           # Source directory to clone from
    variant_target: ad/GOAD-variant-1 # Output directory for generated variant
    variant_name: variant-1           # Variant identifier
  staging:
    variant: false
```

## Per-Environment Settings

The `environments` map lets you configure behavior per environment. The
active environment is selected by the top-level `env` key.

### Variant Support

When `variant: true`, the environment uses a randomized GOAD variant
instead of the stock lab. Variants are graph-isomorphic copies with
randomized entity names (domains, users, hosts, groups, OUs, passwords)
that preserve all structural relationships and vulnerabilities.

| Key              | Description                          | Default              |
|------------------|--------------------------------------|----------------------|
| `variant`        | Enable randomized variant            | `false`              |
| `variant_source` | Source GOAD directory to clone from  | `ad/GOAD`            |
| `variant_target` | Output directory for the variant     | `ad/GOAD-variant-1`  |
| `variant_name`   | Variant identifier                   | `variant-1`          |

### How It Works

- **`dreadgoad provision`**: When the active environment has `variant: true`,
  provisioning automatically generates the variant if the target directory
  doesn't exist yet. Subsequent runs skip generation.

- **`dreadgoad variant generate`**: Reads defaults from the active
  environment's config. Explicit flags (`--source`, `--target`, `--name`)
  override the config values.

- **Regenerating**: Delete the variant target directory and re-run
  `dreadgoad provision` or `dreadgoad variant generate` to get fresh
  randomized names.

## Environment Variables

All config keys can be set via environment variables with the
`DREADGOAD_` prefix:

| Variable              | Config Key     |
|-----------------------|----------------|
| `DREADGOAD_ENV`       | `env`          |
| `DREADGOAD_REGION`    | `region`       |
| `DREADGOAD_DEBUG`     | `debug`        |
| `DREADGOAD_MAX_RETRIES` | `max_retries` |
| `DREADGOAD_RETRY_DELAY` | `retry_delay` |
| `DREADGOAD_IDLE_TIMEOUT` | `idle_timeout` |
| `DREADGOAD_LOG_DIR`   | `log_dir`      |
