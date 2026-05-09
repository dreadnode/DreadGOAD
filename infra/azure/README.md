# Azure Auth Validation POC

Throwaway Terragrunt module that stands up a single Windows Server 2022 VM in
Azure (no public IP, no inbound rules) to confirm `az login` credentials flow
through the `azurerm` Terraform provider end-to-end.

This is **not** the eventual Azure provider implementation. It exists only to
prove the auth/state path works before we invest in the full provider parity
work.

## Prerequisites

```sh
az login
az account show          # confirm correct subscription is default
```

If you have multiple subscriptions:

```sh
az account set --subscription <id-or-name>
```

## Apply

```sh
cd infra/azure/eastus/auth-validation
terragrunt init
terragrunt apply
```

State is local to `.terragrunt-cache/` for this POC — no remote backend.

## Verify

```sh
az vm list -g dreadgoad-azure-rg -o table
```

The VM should show `PowerState/running`.

## Tear down

```sh
terragrunt destroy
```

## What this validates

- `az login` creds reach the `azurerm` provider
- We can create RG / VNet / subnet / NSG / NIC / Windows VM
- Random password generation for admin user works
- The `MicrosoftWindowsServer:WindowsServer:2022-datacenter-azure-edition`
  image is available in the chosen region
