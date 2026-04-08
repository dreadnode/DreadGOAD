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

- [Terraform](https://www.terraform.io/downloads.html)
- [Azure CLI](https://docs.microsoft.com/en-us/cli/azure/install-azure-cli?view=azure-cli-latest)

## Azure configuration

You need to login to Azure with the CLI.

```bash
az login
```

## DreadGOAD configuration

- Initialize the configuration file with `dreadgoad config init`
- Azure-specific settings are configured in `dreadgoad.yaml`:

```yaml
# dreadgoad.yaml
azure:
  location: westeurope
```

- If you want to use a different location you can modify it.


## Installation

```bash
# check prerequisites
dreadgoad doctor
# Create cloud infrastructure
dreadgoad infra apply
# Sync inventory
dreadgoad inventory sync
# Provision the lab
dreadgoad provision
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
dreadgoad doctor                # check prerequisites
dreadgoad infra apply           # create cloud infrastructure with Terragrunt/Terraform
dreadgoad inventory sync        # sync inventory and sources to jumpbox
dreadgoad provision             # run Ansible provisioning via jumpbox
```

## Tips

- To connect to a host you can use `dreadgoad ssm connect <host>`

- If the command `destroy` or `delete` fails, you can delete the resource group using the CLI

```bash
az group delete --name GOAD
```
