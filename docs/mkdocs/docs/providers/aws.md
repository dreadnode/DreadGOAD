# :simple-amazon: Aws

!!! success "Thanks!"
    Thx to @ArnC_CarN for the initial work on the aws provider

<div align="center">
  <img alt="terraform" width="167" height="150" src="./../img/icon_terraform.png">
  <img alt="icon_aws" width="193"  height="150" src="./../img/icon_aws.png">
  <img alt="icon_ansible" width="150"  height="150" src="./../img/icon_ansible.png">
</div>

The architecture is quite the same than the Azure deployment.

![Architecture](../img/aws_schema.png)

!!! Warning
    LLMNR, NBTNS and other poisoning network attacks will not work in aws environment.
    Only network coerce attacks will work.

## Prerequisites

- [Terraform](https://www.terraform.io/downloads.html)
- [AWS CLI](https://aws.amazon.com/cli/?nc1=h_ls)
- [AWS Session Manager plugin](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html)

To use the optional attack box, subscribe to the official
[Kali Linux AWS Marketplace image](https://aws.amazon.com/marketplace/pp/prodview-fznsw3f7mq7to)
once in the target AWS account. DreadGOAD selects the latest
`*kali-last-snapshot-amd64-*-804fcc46-63fc-4eb6-85a1-50e66d6c7215`
image from `aws-marketplace`, constraining discovery to Kali's official
Marketplace product ID.

## AWS configuration

You need to configure AWS cli. Use a key with enough privileges on the tenant.

```bash
aws configure
```

- Create an AWS access key and secret for DreadGOAD usage
    - Go to IAM > User > your user > Security credentials
    - Click the Create access key button
    - Create a group "[goad]" in credentials file ~/.aws/credentials

        ```ini
        [goad]
        aws_access_key_id = changeme
        aws_secret_access_key = changeme
        ```

    - Be sure to chmod 400 the file

    !!! warning "credentials in plain text"
        Storing credentials in plain text is always a bad idea, but aws cli work like that be sure to restrain the right access to this file

## DreadGOAD configuration

- Initialize the configuration file with `dreadgoad config init`
- AWS-specific settings are configured in `dreadgoad.yaml`:

```yaml
# dreadgoad.yaml
aws:
  region: eu-west-3
  zone: eu-west-3c
  whitelist_cidr: "YOUR.PUBLIC.IP/32"
```

- `whitelist_cidr` (**required**): Your public IP in CIDR notation (e.g. `203.0.113.10/32`). This controls which IP can access the jumpbox. Terraform will fail if this is not set.
- If you want to use a different region and zone you can modify it (but this could break ami values in terraform)

## Change AMI value

- You can get the ami of Windows_Server with a query like that:

```bash
aws ec2 describe-images \
  --region eu-west-3 \
  --owners 801119661308 \
  --filters "Name=name,Values=Windows_Server-2016-English-Full-Base*" \
  --query 'Images[*].[ImageId,Name,CreationDate]' \
  --output table
```

- for ubuntu use this command :

```bash
aws ec2 describe-images \
  --region eu-west-3 \
  --owners 099720109477 \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*" \
  --query "Images[*].[ImageId,Name,CreationDate]" \
  --output table
```

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

Include the optional private Kali attack box with:

```bash
dreadgoad infra apply --with-kali
```

The attack box is tagged `Role=AttackBox`, bootstraps the SSM agent and live
scoring tools, and has no public IP or inbound SSH rule. Connect without adding
it to the Ansible inventory:

```bash
dreadgoad ssm connect attack-box
```

## start/stop/status

- You can see the status of the lab with `dreadgoad lab status`
- You can also start and stop the lab with `dreadgoad lab start` and `dreadgoad lab stop`


## VMs ami

- The VMs used for DreadGOAD are defined in the lab terraform file: `ad/<lab>/providers/aws/windows.tf`
- This file is containing information about each vm in use

```hcl
"dc01" = {
  name               = "dc01"
  domain             = "sevenkingdoms.local"
  windows_sku        = "2019-Datacenter"
  ami                = "ami-018ebfbd6b0a4c605"
  instance_type      = "t2.medium"
  private_ip_address = "{{ip_range}}.10"
  password           = "8dCT-DJjgScp"
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

- To connect to a host via SSM you can use `dreadgoad ssm connect <host>`
- `dreadgoad doctor` checks both the AWS CLI and Session Manager plugin so a
  missing local plugin is reported before an interactive session is attempted.
- All AWS elements are tagged with `<lab_name>-<lab_instance_id>`
