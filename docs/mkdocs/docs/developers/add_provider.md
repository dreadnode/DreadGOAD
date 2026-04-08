# Add a new provider

Adding a new provider involves creating provider-specific files for each lab and updating the DreadGOAD CLI and infrastructure modules.

## Provider files

- Add the new provider files in each lab location: `ad/<lab>/providers/<provider_name>`
- Add the new provider files in each extension location: `extensions/<extension>/providers/<provider_name>`
- Create the provider templates file in: `template/provider/<provider_name>`

## Provider architecture

DreadGOAD uses a Go CLI (`dreadgoad`) for all provider orchestration. The provider logic lives in `cli/cmd/` and infrastructure is managed through Terragrunt modules in `infra/`.

There are two main provider patterns:

### Vagrant-based providers (virtualbox, vmware)

Vagrant-based providers use a `Vagrantfile` in `ad/<lab>/providers/<provider_name>/` to define and manage VMs. The Vagrantfile specifies:

- VM names, memory, and CPU allocations
- Network interfaces and IP assignments
- Linked clones from base boxes
- WinRM communicator settings

Each lab's Vagrantfile is self-contained. The CLI invokes Vagrant commands (`vagrant up`, `vagrant halt`, `vagrant destroy`, etc.) to manage the VM lifecycle.

Provider directory structure:

```text
ad/<lab>/providers/<provider_name>/
    Vagrantfile     # VM definitions
    inventory       # Ansible inventory with provider-specific IPs and connection settings
```

### Terraform/Terragrunt-based providers (aws, azure, proxmox)

Terraform-based providers define infrastructure as `.tf` files in `ad/<lab>/providers/<provider_name>/` and use shared Terragrunt modules from `infra/` for deployment orchestration.

Each lab's provider directory contains:

```text
ad/<lab>/providers/<provider_name>/
    windows.tf      # Windows VM resource definitions
    linux.tf        # Linux VM resource definitions (if needed)
    inventory       # Ansible inventory with provider-specific connection settings
```

The `infra/` directory contains Terragrunt root configuration (`root.hcl`) and deployment modules (`infra/goad-deployment/`) that handle:

- VPC/network setup
- Security groups and firewall rules
- VM provisioning from AMIs or cloud images
- Jumpbox/provisioning host setup

## Adding a new provider

1. **Create provider directories** in each lab under `ad/<lab>/providers/<provider_name>/` with the appropriate files (Vagrantfile or .tf files) and a provider-specific `inventory` file.

2. **Update CLI support** in `cli/cmd/` to handle the new provider:
    - Add provider name to the allowed providers list in the configuration (`cli/internal/config/`)
    - Add any provider-specific commands or flags
    - Implement health checks in `doctor.go` for provider prerequisites

3. **For Terragrunt providers**: add or extend modules in `infra/` to support the new cloud platform.

4. **Add provider templates** in `template/provider/<provider_name>` so new labs can be scaffolded with the correct provider files.

5. **Test** with `dreadgoad doctor` to verify prerequisites, then `dreadgoad provision` to run a full deployment.
