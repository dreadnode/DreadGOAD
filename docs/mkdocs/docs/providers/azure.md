# :material-microsoft-azure: Azure

!!! success "Thanks!"
    Thx to Julien Arault for the initial work on the azure provider


<div align="center">
  <img alt="terraform" width="167" height="150" src="./../img/icon_terraform.png">
  <img alt="icon_azure" width="160"  height="150" src="./../img/icon_azure.png">
  <img alt="icon_ansible" width="150"  height="150" src="./../img/icon_ansible.png">
</div>

![Architecture](../img/azure_architecture.png)

!!! Warning
    LLMNR, NBTNS and other poisoning network attacks will not work in azure environment.
    Only network coerce attacks will work.

## Prerequisites

- [OpenTofu](https://opentofu.org/) or [Terraform](https://www.terraform.io/downloads.html)
- [Terragrunt](https://terragrunt.gruntwork.io/)
- [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli?view=azure-cli-latest)

## Azure configuration

You need to login to Azure with the CLI.

```bash
az login
```

## DreadGOAD configuration

The Azure path is selected at invocation time via `-p azure` and `--region <azure-region>` (e.g. `centralus`). These can also be set in `dreadgoad.yaml` to avoid re-passing them:

```yaml
# dreadgoad.yaml
provider: azure
region: centralus
env: test
```

!!! note "Azure region naming"
    Azure regions use names like `centralus`, `westeurope`, `eastus2` — not AWS-style names like `us-east-2`. Passing an AWS region routes the apply down the wrong provider path.

!!! warning "Subscription capacity"
    The Dreadnode MSFT Startup subscription has tight quota limits in `eastus`. Default to `centralus` unless you know the target region has capacity.


## Installation

```bash
# check prerequisites
dreadgoad doctor
# Create cloud infrastructure (Azure)
dreadgoad infra apply -p azure --env test --region centralus --auto-approve
# Provision the lab (TBD: Ansible WinRM transport)
dreadgoad provision -p azure --env test --region centralus
```

## start/stop/status

- You can see the status of the lab with `dreadgoad lab status`
- You can also start and stop the lab with `dreadgoad lab start` and `dreadgoad lab stop`

!!! info
    The command `stop` use deallocate, it take a long time to run but it is not only stopping the vms, it will deallocate them. By doing that, you will stop paying from them (but you still paying storage) and can save some money.

## VMs sku

- The VMs used for DreadGOAD are defined in the lab terraform file: `ad/<lab>/providers/azure/windows.tf`
- This file is containing information about each vm in use

```hcl
"dc01" = {
  name               = "dc01"
  publisher          = "MicrosoftWindowsServer"
  offer              = "WindowsServer"
  windows_sku        = "2019-Datacenter"
  windows_version    = "17763.4377.230505"
  private_ip_address = "{{ip_range}}.10"
  password           = "8dCT-DJjgScp"
  size               = "Standard_B2s"
}
```

## How it works ?

- The DreadGOAD CLI uses Terragrunt/Terraform to create the cloud infrastructure (`dreadgoad infra apply`)
- The lab is created (not provisioned yet) and a "jumpbox" VM is also created
- Next the needed sources will be pushed to the jumpbox using `ssh` and `rsync`
- The jumpbox is prepared to run Ansible
- The provisioning is launched with SSH remotely on the jumpbox

## Install step by step

```bash
dreadgoad doctor                                                                # check prerequisites
dreadgoad infra apply -p azure --env test --region centralus --auto-approve     # create cloud infrastructure with Terragrunt/Terraform
dreadgoad provision  -p azure --env test --region centralus                     # run Ansible provisioning (transport: see "How it works")
```

## Remote command access (`runcmd`)

Azure does not have a direct equivalent of AWS SSM Session Manager. The closest match without deploying Azure Bastion is **Azure Run Command** (control-plane, no inbound ports). DreadGOAD exposes this under a separate verb so it isn't confused with AWS SSM:

| Operation | AWS | Azure |
|---|---|---|
| Run a one-shot command on hosts | `dreadgoad ssm run -c '<ps>'` | `dreadgoad runcmd run -c '<ps>'` |
| Open an interactive shell | `dreadgoad ssm connect <host>` | `dreadgoad runcmd connect <host>` |
| List active sessions | `dreadgoad ssm status` | _N/A — Run Command is stateless_ |
| Clean up stale sessions | `dreadgoad ssm cleanup` | _N/A — Run Command is stateless_ |

!!! warning "`runcmd connect` is a REPL, not a true interactive shell"
    Each line you type becomes a separate Azure Run Command invocation. For a real-time shell, deploy the optional Azure Bastion module (`dreadgoad infra apply --with-bastion`) and use `dreadgoad bastion ssh|rdp|tunnel`. We don't bundle Bastion in the default GOAD module because it adds ~$140/mo per environment.

REPL caveats:

- **~5-15s latency per command** (Run Command's end-to-end round-trip)
- **Output capped at 4096 bytes per stream** (Azure-imposed; pipe big output through `Out-File` and pull the file separately)
- `$PWD` is persisted between invocations (`cd` works across lines)
- No live stdin, no signal forwarding to the remote process, no streaming output
- Ctrl+C cancels the in-flight invocation but keeps the session open
- Type `exit` (or send EOF) to leave

```bash
# Run across all hosts
dreadgoad runcmd run -c 'Get-Service WinRM | Format-List Status, StartType'

# Run on specific hosts
dreadgoad runcmd run --hosts dc01,srv01 -c 'whoami'

# Open a REPL to a single host
dreadgoad runcmd connect dc01
```

## Native-client access via Azure Bastion (`bastion`)

For real-time interactive sessions and port tunneling, DreadGOAD ships an optional Azure Bastion module (`modules/terraform-azure-bastion/`). It is excluded from the default lab apply because it adds ~$140/mo. Enable it by passing `--with-bastion` to `infra apply`, or by exporting `DREADGOAD_ENABLE_AZURE_BASTION=true` before running Terragrunt directly.

```bash
# Deploy lab + Bastion together
dreadgoad infra apply --with-bastion -p azure --env test --region centralus --auto-approve

# Or deploy Bastion alone after the lab is up
dreadgoad infra apply --with-bastion --module bastion -p azure --env test --region centralus

# Status of the deployed Bastion
dreadgoad bastion status
```

The Bastion module defaults to SKU `Standard` with `tunneling_enabled = true`, which is the minimum needed for `bastion ssh|rdp|tunnel`. The `Developer` SKU only supports the browser console.

| Operation | Command |
|---|---|
| Open SSH to a Linux/jumpbox VM | `dreadgoad bastion ssh <host> --user <u>` |
| Open RDP to a Windows VM (Windows clients only) | `dreadgoad bastion rdp <host>` |
| Tunnel a port (default RDP/3389) for any client | `dreadgoad bastion tunnel <host> --remote-port 3389 --local-port 3389` |

Hostname resolution mirrors `runcmd`: the inventory and live tag-based discovery are both consulted. The discovered Bastion host is found by tag (`Project=DreadGOAD`, `Environment=<env>`).

```bash
# RDP through Bastion via a local-port tunnel from a non-Windows client
dreadgoad bastion tunnel dc01 --remote-port 3389 --local-port 3389
# (in another terminal)
xfreerdp /v:localhost:3389 /u:Administrator
```

!!! note "Tunneling SKU requirement"
    `bastion ssh|rdp|tunnel` require `tunneling_enabled = true` on a `Standard` or `Premium` SKU. `dreadgoad bastion status` warns when the deployed Bastion lacks tunneling.

## Tips

- If the command `destroy` or `delete` fails, you can delete the resource group using the CLI

```bash
az group delete --name GOAD
```
