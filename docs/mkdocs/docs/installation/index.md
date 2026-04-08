<!-- markdownlint-disable MD046 -->
# Installation

DreadGOAD uses a self-contained Go CLI binary (`dreadgoad`) for all management operations. The CLI handles infrastructure provisioning, Ansible orchestration, inventory management, and environment validation. The only external dependencies are Ansible (for provisioning) and your chosen provider's tools (e.g., AWS CLI, Terraform).

- First prepare your system for DreadGOAD execution:
    - :material-linux: [Linux](linux.md)
    - :material-microsoft-windows: [Windows](windows.md)

- Installation depends on the provider you use, please follow the appropriate guide:
    - :simple-virtualbox: [Install with Virtualbox](../providers/virtualbox.md)
    - :simple-vmware: [Install with VmWare](../providers/vmware.md)
    - :simple-proxmox: [Install with Proxmox](../providers/proxmox.md)
    - :material-microsoft-azure: [Install with Azure](../providers/azure.md)
    - :simple-amazon: [Install with Aws](../providers/aws.md)
    - 🏟️ [Install with Ludus](../providers/ludus.md)

## TLDR - quick install

??? info "TLDR : :simple-amazon: AWS quick install"

    ```bash
    # Install dreadgoad CLI (download from releases or build from source)
    # See https://github.com/dreadnode/DreadGOAD/releases for latest binaries
    # Or build from source:
    git clone https://github.com/dreadnode/DreadGOAD.git
    cd DreadGOAD
    cd cli && go build -o dreadgoad . && sudo mv dreadgoad /usr/local/bin/

    # Initialize configuration
    dreadgoad config init

    # Check dependencies (ansible-core, AWS CLI, Python for Ansible, collections)
    dreadgoad doctor

    # Create a deployment environment
    dreadgoad env create dev

    # Provision infrastructure
    dreadgoad infra init && dreadgoad infra apply

    # Sync inventory
    dreadgoad inventory sync

    # Run Ansible provisioning
    dreadgoad provision

    # Validate the lab
    dreadgoad validate
    ```

## Installation Steps

- Installation is in three parts:
    - Templating: this will create the template to use (needed only for proxmox and ludus)
    - Providing: this will instantiate the virtual machines depending on your provider
    - Provisioning: it is always made with ansible, it will install all the stuff to create the lab

- The `dreadgoad` CLI covers the providing and provisioning parts through subcommands:
    - `dreadgoad infra init` / `dreadgoad infra apply` - provision infrastructure
    - `dreadgoad provision` - run Ansible provisioning
    - `dreadgoad validate` - validate the deployment

### Dependencies

`dreadgoad` is a self-contained Go binary with no runtime dependencies of its own. However, the following external tools are required depending on your workflow:

- **Always required**: `ansible-core`, Python (for Ansible)
- **For AWS provider**: AWS CLI, Terraform/Terragrunt
- **For Azure provider**: Azure CLI, Terraform
- **For Virtualbox/VMware**: Vagrant and appropriate plugins
- **For Proxmox**: Proxmox API access

Run `dreadgoad doctor` to check that all required dependencies are installed and configured.

## Configuration files

### dreadgoad.yaml

- On first setup, run `dreadgoad config init` to create a configuration file at `~/.config/dreadgoad/dreadgoad.yaml`.
- View the current effective configuration with `dreadgoad config show`.
- Set individual values with `dreadgoad config set <key> <value>`.

The config file uses YAML format and supports the following resolution order:

1. CLI flags (`--env`, `--region`, `--debug`)
2. Environment variables (`DREADGOAD_ENV`, `DREADGOAD_REGION`, etc.)
3. Config file (YAML)
4. Built-in defaults

```yaml
# Active environment (selects into the environments map below)
env: staging

# AWS region override (default: resolved from inventory)
# region: us-west-2

debug: false
max_retries: 3  # Ansible playbook retry attempts
```

### Global configuration : globalsettings.ini

- DreadGOAD has a global configuration file: `globalsettings.ini` used by the ansible provisioning
- This file is an ansible inventory file.
- This file is always added at the end of the ansible inventory file list so you can override values here
- You can change it before running the installation to modify:
    - keyboard_layouts
    - proxy configuration
    - add a route to the vm
    - change the default dns_forwarder
    - disable ssl for winrm communication
