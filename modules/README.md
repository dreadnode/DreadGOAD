# Terraform Modules

Reusable Terraform modules used by the DreadGOAD Terragrunt configurations.

## Modules

### [terraform-azure-net](terraform-azure-net/)

Azure VNet and shared networking infrastructure for GOAD labs.

Key features:

- Dedicated resource group and VNet for lab workloads
- Private subnet for lab VMs plus optional public subnet
- NAT-backed outbound internet for private Windows hosts
- Private subnet NSG tuned for broad intra-lab AD connectivity
- Terragrunt-compatible structure

### [terraform-azure-instance-factory](terraform-azure-instance-factory/)

Azure VM provisioning module for individual GOAD hosts.

Key features:

- Single Windows VM provisioning into an existing subnet
- Optional bootstrap script via Custom Script Extension
- Optional public IP for jumpbox-style access
- System-assigned managed identity for Azure Run Command workflows
- Terragrunt-compatible structure

### [terraform-azure-bastion](terraform-azure-bastion/)

Optional Azure Bastion deployment for operators who want browser-based or
native-client access to private Azure lab VMs.

Key features:

- Dedicated Bastion support for `Basic`, `Standard`, and `Premium`
- Developer SKU support for lightweight dev/test access
- Optional creation of `AzureBastionSubnet` and required public IP
- Validation around SKU/feature compatibility
- Terragrunt-compatible structure

### [terraform-azure-controller](terraform-azure-controller/)

Optional in-VNet Linux Ansible controller. Closes the gap that Azure has no
first-class equivalent of `amazon.aws.aws_ssm`: targets stay private and
Ansible runs over normal SSH/WinRM/PSRP from inside the VNet, with one
human-facing Bastion tunnel into the controller.

Key features:

- Ubuntu 24.04 LTS VM with no public IP, in a dedicated subnet
- NSG locks SSH ingress to the AzureBastionSubnet only
- cloud-init bootstraps `ansible-core`, `pywinrm`, `pypsrp`, and the GOAD
  Galaxy collections into `/opt/ansible-venv`
- SSH key authentication only — no password auth
- Terragrunt-compatible structure

### [terraform-aws-instance-factory](terraform-aws-instance-factory/)

Flexible EC2 instance provisioning supporting Linux, Windows, and macOS. Used by each GOAD host's `terragrunt.hcl` to deploy domain controllers and member servers.

Key features:

- AMI selection with configurable filters (used to select pre-baked warpgate AMIs)
- SSM-based management (no SSH keys or open ports)
- IAM instance profiles with SSM permissions
- Encrypted EBS volumes
- Security group management
- Optional ASG, ALB, and NLB support

### [terraform-aws-net](terraform-aws-net/)

AWS VPC and networking infrastructure. Used by the `network/terragrunt.hcl` to create the lab network.

Key features:

- VPC with public and private subnets across multiple AZs
- NAT Gateway for outbound internet access
- VPC endpoints for AWS services (SSM, Secrets Manager, ECR, CloudWatch, S3)
- Security groups for VPC endpoint access
- Terragrunt-compatible structure

## How They're Used

The Terragrunt configurations under `infra/*/goad-deployment/` reference these modules:

```text
infra/{provider}/goad-deployment/{env}/{region}/
├── network/terragrunt.hcl    → uses terraform-aws-net or terraform-azure-net
├── bastion/terragrunt.hcl    → optional; uses terraform-azure-bastion
├── controller/terragrunt.hcl → optional; uses terraform-azure-controller
└── goad/
    ├── dc01/terragrunt.hcl   → uses terraform-aws-instance-factory or terraform-azure-instance-factory
    ├── dc02/terragrunt.hcl   → uses terraform-aws-instance-factory or terraform-azure-instance-factory
    ├── dc03/terragrunt.hcl   → uses terraform-aws-instance-factory or terraform-azure-instance-factory
    ├── srv02/terragrunt.hcl  → uses terraform-aws-instance-factory or terraform-azure-instance-factory
    └── srv03/terragrunt.hcl  → uses terraform-aws-instance-factory or terraform-azure-instance-factory
```

Each module has its own detailed README with full input/output documentation.
