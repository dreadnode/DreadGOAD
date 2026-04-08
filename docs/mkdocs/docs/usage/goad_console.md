# Migration from GOAD Console

DreadGOAD replaces the GOAD interactive console (`goad.sh` / `goad.py`) with a structured CLI. There is no interactive mode -- all operations are run as standalone commands.

## Command mapping

The table below maps every old console command to its DreadGOAD equivalent.

| Old Console Command | New CLI Command |
|---|---|
| `check` | `dreadgoad doctor` |
| `install` | `dreadgoad infra apply && dreadgoad provision` |
| `status` | `dreadgoad lab status` |
| `start` | `dreadgoad lab start` |
| `stop` | `dreadgoad lab stop` |
| `destroy` | `dreadgoad infra destroy` |
| `start_vm <name>` | `dreadgoad lab start-vm <name>` |
| `stop_vm <name>` | `dreadgoad lab stop-vm <name>` |
| `restart_vm <name>` | `dreadgoad lab restart-vm <name>` |
| `destroy_vm <name>` | `dreadgoad lab destroy-vm <name>` |
| `list_extensions` | `dreadgoad extension list` |
| `install_extension <ext>` | `dreadgoad extension provision <ext>` |
| `provision_extension <ext>` | `dreadgoad extension provision <ext>` |
| `provision <playbook>` | `dreadgoad provision --plays <playbook>` |
| `provision_lab` | `dreadgoad provision` |
| `provision_lab_from <pb>` | `dreadgoad provision --from <pb>` |
| `config` | `dreadgoad config show` |
| `labs` | `dreadgoad lab list` |

## Key differences

- **No interactive session.** Every command runs to completion and exits. Chain commands with `&&` when you need multi-step workflows.
- **Environments replace instances.** Use `dreadgoad env create <name>` to create an environment and `--env <name>` to target it.
- **Configuration is file-based.** Run `dreadgoad config init` to generate a `dreadgoad.yaml` config, then `dreadgoad config set` or edit the file directly.
- **Provider is always AWS.** DreadGOAD targets AWS exclusively; there are no provider flags.
- **No jumpbox commands.** SSM replaces SSH-based jumpbox access. Use `dreadgoad ssm connect` and `dreadgoad ssm run` instead.

## Example: migrating a typical workflow

Old GOAD console session:

```text
./goad.sh
> set_lab GOAD
> set_provider aws
> check
> install
> status
```

Equivalent DreadGOAD commands:

```bash
dreadgoad doctor
dreadgoad infra apply
dreadgoad provision
dreadgoad lab status
```
