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
    vpc_cidr: "10.0.0.0/16"          # VPC CIDR block for this environment
  staging:
    variant: false
    vpc_cidr: "10.1.0.0/16"
  prod:
    vpc_cidr: "10.2.0.0/16"
  test:
    vpc_cidr: "10.8.0.0/16"
```

## Per-Environment Settings

The `environments` map lets you configure behavior per environment. The
active environment is selected by the top-level `env` key.

### VPC CIDR

Each environment needs a unique VPC CIDR block. Set `vpc_cidr` in the
environment config -- this value is used by `dreadgoad env create` when
scaffolding Terragrunt files and must match the `vpc_cidr` in the
corresponding `env.hcl`.

If `vpc_cidr` is not set and no `--vpc-cidr` flag is passed, the CLI
generates a deterministic CIDR from the environment name.

### Variant Support

When `variant: true`, the environment uses a randomized GOAD variant
instead of the stock lab. Variants are graph-isomorphic copies with
randomized entity names (domains, users, hosts, groups, OUs, passwords)
that preserve all structural relationships and vulnerabilities.

| Key              | Description                          | Default              |
|------------------|--------------------------------------|----------------------|
| `vpc_cidr`       | VPC CIDR block for this environment  | Auto-generated       |
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

## Lab Config Overlays

Each lab stores its canonical configuration in a single `config.json` file
(e.g. `ad/GOAD/data/config.json`). Per-environment differences are captured
in small **overlay** files rather than full copies:

```text
ad/GOAD/data/
├── config.json              # Single source of truth (~32 KB)
├── dev-overlay.json         # Only the dev-specific diffs (~1.8 KB)
├── staging-overlay.json     # Only the staging-specific diffs
└── test-overlay.json        # Only the test-specific diffs
```

Overlays use [RFC 7386 JSON Merge Patch](https://datatracker.ietf.org/doc/html/rfc7386)
semantics:

- **Objects** merge recursively — only changed keys need to appear.
- **Arrays and scalars** in the overlay replace the base value wholesale.
- **`null`** removes a key from the base.

For example, a `dev-overlay.json` that removes ADCS vulns from `dc01` and
adds a script to `dc02`:

```json
{
  "lab": {
    "hosts": {
      "dc01": { "vulns": ["disable_firewall"] },
      "dc02": { "scripts": ["...", "unconstrained_delegation_user.ps1"] }
    }
  }
}
```

### Resolution order

At runtime `dreadgoad` resolves the lab config for the active environment as:

1. `{env}-overlay.json` exists → merge `config.json` + overlay, cache result
   in `.dreadgoad/cache/{env}-config.json`
2. Legacy `{env}-config.json` exists → use it directly (backward compatible)
3. Neither exists → use `config.json` as-is

Variant environments follow the same logic but read from the variant target
directory (e.g. `ad/GOAD-variant-1/data/`).

### Creating overlays for a new environment

`dreadgoad env create <name>` creates an overlay file automatically:

- **Without `--variant`**: copies `dev-overlay.json` as a starting template
  (or creates an empty `{}` overlay).
- **With `--variant`**: generates a full randomized variant; overlay files
  in the source are also transformed through the variant's replacement
  pipeline.

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
