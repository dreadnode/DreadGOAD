# AWS Network Terraform Module

<div align="center">

<img
  src="https://d1lppblt9t2x15.cloudfront.net/logos/5714928f3cdc09503751580cffbe8d02.png"
  alt="Logo"
  align="center"
  width="144px"
  height="144px"
/>

## Terraform module for AWS VPC and Network Infrastructure ☁️

_... managed with Terraform, Terratest, and GitHub Actions_ 🤖

</div>

<div align="center">

[![Terratest](https://github.com/dreadnode/terraform-aws-net/actions/workflows/terratest.yaml/badge.svg)](https://github.com/dreadnode/terraform-aws-net/actions/workflows/terratest.yaml)
[![Pre-Commit](https://github.com/dreadnode/terraform-aws-net/actions/workflows/pre-commit.yaml/badge.svg)](https://github.com/dreadnode/terraform-aws-net/actions/workflows/pre-commit.yaml)
[![Renovate](https://github.com/dreadnode/terraform-aws-net/actions/workflows/renovate.yaml/badge.svg)](https://github.com/dreadnode/terraform-aws-net/actions/workflows/renovate.yaml)

</div>

---

## 📖 Overview

This Terraform module creates a complete AWS networking foundation with public
and private subnets across multiple Availability Zones. It includes:

- A VPC with DNS support and hostnames enabled
- Public subnets with route to Internet Gateway
- Private subnets with route to NAT Gateway
- NAT Gateway with Elastic IP for outbound internet access
- Configurable VPC endpoints for AWS services (Interface and Gateway types)
- Security groups for VPC endpoints with customizable ingress/egress rules
- Automatic AZ distribution for subnet placement
- Route tables for both public and private subnets
- Optional Kubernetes integration with cluster-specific resource tagging

The module is designed to provide a secure and scalable network architecture
that follows AWS best practices. All resources are tagged consistently, and the
configuration can be customized through variables to support different
environments and requirements.

Key features:

- Multi-AZ deployment for high availability
- Separate public and private subnet tiers
- Configurable CIDR blocks and subnet sizes
- Optional VPC endpoints for AWS service access
- Flexible security group rules for endpoint access
- Kubernetes integration for EKS clusters
- Terragrunt-compatible structure

---

## Table of Contents

- [Features](#features)
- [Usage](#usage)
- [Inputs](#inputs)
- [Outputs](#outputs)
- [Requirements](#requirements)
- [Development](#development)

---

## Features

This Terraform module deploys the following AWS resources:

### VPC Endpoints (Optional)

- Support for both Interface and Gateway endpoint types
- Automatic security group creation and management for Interface endpoints
- Private DNS configuration for Interface endpoints
- Route table associations for Gateway endpoints
- Customizable endpoint access through security group rules

### Security & Access

- Configurable public IP mapping for instances in public subnets
- Default security group with customizable name
- VPC endpoint security groups with configurable ingress/egress rules
- Separation of public and private resources across subnet tiers

### Kubernetes Integration (Optional)

- Automatic tagging of VPC and subnet resources for Kubernetes integration
- Support for EKS load balancer controller with appropriate subnet role tags
- Configurable cluster name for resource tagging
- Public subnet tagging for external load balancers
- Private subnet tagging for internal load balancers

### Infrastructure Management

- Multi-AZ deployment for high availability
- Automatic AZ distribution for subnet placement
- Flexible tagging system for all resources
- Terragrunt-compatible structure
- Lifecycle management with create_before_destroy support

### Customization Options

- Configurable CIDR blocks for VPC and subnets
- Adjustable subnet sizes and distribution
- Optional VPC endpoint deployment
- Customizable security group rules
- Flexible tagging system for resource management

---

## Usage

### Basic Network Setup

```hcl
module "network" {
  source          = "github.com/dreadnode/terraform-aws-network"
  env             = "dev"
  deployment_name = "my-crucible"
  vpc_cidr_block  = "10.0.0.0/16"
  public_subnet_cidrs  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnet_cidrs = ["10.0.3.0/24", "10.0.4.0/24", "10.0.32.0/21", "10.0.40.0/21"]
  map_public_ip   = true
  additional_tags = {
    Project = "MyProject"
  }
}
```

### With VPC Endpoints

This example shows how to enable VPC endpoints for common AWS services with
custom security group rules:

```hcl
module "network" {
  source          = "github.com/dreadnode/terraform-aws-network"
  env             = "dev"
  deployment_name = "my-crucible"
  vpc_cidr_block  = "10.0.0.0/16"
  public_subnet_cidrs  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnet_cidrs = ["10.0.3.0/24", "10.0.4.0/24", "10.0.32.0/21", "10.0.40.0/21"]
  env             = "dev"
  deployment_name = "my-crucible"
  vpc_cidr_block  = "10.0.0.0/16"
  public_subnet_cidrs  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnet_cidrs = ["10.0.3.0/24", "10.0.4.0/24", "10.0.32.0/21", "10.0.40.0/21"]

  # VPC Endpoint configurations
  vpc_endpoints = {
    secretsmanager = {
      service = "secretsmanager"
      type    = "Interface"
      security_group_ids = []  # Module will create and manage security group
    }
    ecr_dkr = {
      service = "ecr.dkr"
      type    = "Interface"
      security_group_ids = []
    }
    ecr_api = {
      service = "ecr.api"
      type    = "Interface"
      security_group_ids = []
    }
    cloudwatch = {
      service = "logs"
      type    = "Interface"
      security_group_ids = []
    }
    s3 = {
      service = "s3"
      type    = "Gateway"
      security_group_ids = []  # Not used for Gateway endpoints
    }
  }

  # Security group rules for VPC endpoints
  vpce_security_group_rules = {
    ingress_security_group_ids = []  # Add SG IDs that need access to endpoints
    ingress_cidr_blocks = ["10.0.0.0/16"]  # Allow access from within VPC
    egress_cidr_blocks = ["0.0.0.0/0"]
  }

  additional_tags = {
    Project = "MyProject"
  }
}
```

### With Kubernetes Integration

This example shows how to enable Kubernetes-specific tagging for EKS integration:

```hcl
module "network" {
  source          = "github.com/dreadnode/terraform-aws-network"
  env             = "dev"
  deployment_name = "my-crucible"
  vpc_cidr_block  = "10.0.0.0/16"
  public_subnet_cidrs  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnet_cidrs = ["10.0.3.0/24", "10.0.4.0/24", "10.0.32.0/21", "10.0.40.0/21"]

  # Enable Kubernetes tagging with custom cluster name
  kubernetes_tags = {
    enabled      = true
    cluster_name = "my-eks-cluster"  # Optional - defaults to {env}-{deployment_name}
  }

  additional_tags = {
    Project = "MyProject"
  }
}
```

### Outputs

The module provides several useful outputs:

```hcl
# VPC Information
output "vpc_id" {
  value = module.network.vpc_id
}

# Subnet IDs
output "private_subnet_ids" {
  value = module.network.private_subnet_ids
}

output "public_subnet_ids" {
  value = module.network.public_subnet_ids
}

# VPC Endpoints
output "vpc_endpoints" {
  value = module.network.vpc_endpoints
}

# Security Group for VPC Endpoints
output "vpce_security_group" {
  value = module.network.vpce_security_group
}
```

### Notes

- VPC endpoints are optional - the module will only create them if the
  `vpc_endpoints` variable is populated
- The module automatically creates and manages security groups for Interface
  endpoints
- For Gateway endpoints (like S3), security groups are not required or used
- You can control access to the endpoints through the
  `vpce_security_group_rules` variable
- All endpoints are placed in private subnets by default
- When Kubernetes tagging is enabled, the following tags are applied:
  - To all resources: `kubernetes.io/cluster/<cluster_name>` = "owned"
  - To public subnets: `kubernetes.io/role/elb` = "1"
  - To private subnets: `kubernetes.io/role/internal-elb` = "1"
- These tags allow AWS load balancer controller to automatically discover
  and use the appropriate subnets

---

<!-- markdownlint-disable -->
<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | ~> 1.7 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | ~> 6.42.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_aws"></a> [aws](#provider\_aws) | 6.42.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [aws_default_security_group.default](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/default_security_group) | resource |
| [aws_eip.nat](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/eip) | resource |
| [aws_internet_gateway.main](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/internet_gateway) | resource |
| [aws_nat_gateway.main](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/nat_gateway) | resource |
| [aws_route.additional_private](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route) | resource |
| [aws_route.private_nat_gateway](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route) | resource |
| [aws_route_table.private](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) | resource |
| [aws_route_table.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table) | resource |
| [aws_route_table_association.pod](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_route_table_association.private](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_route_table_association.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/route_table_association) | resource |
| [aws_security_group.vpce](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_subnet.pod](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_subnet.private](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_subnet.public](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/subnet) | resource |
| [aws_vpc.main](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc) | resource |
| [aws_vpc_endpoint.endpoints](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_endpoint) | resource |
| [aws_vpc_ipv4_cidr_block_association.secondary](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/vpc_ipv4_cidr_block_association) | resource |
| [aws_availability_zones.available](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/availability_zones) | data source |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |
| [aws_route_tables.private](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/route_tables) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_private_routes"></a> [additional\_private\_routes](#input\_additional\_private\_routes) | Additional routes to add to the private route table | <pre>map(object({<br/>    destination_cidr_block    = string<br/>    network_interface_id      = optional(string, null)<br/>    gateway_id                = optional(string, null)<br/>    nat_gateway_id            = optional(string, null)<br/>    transit_gateway_id        = optional(string, null)<br/>    vpc_peering_connection_id = optional(string, null)<br/>  }))</pre> | `{}` | no |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Additional tags to apply to resources | `map(string)` | `{}` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Name of the deployment (ex: "crucible") | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | The environment name (e.g., dev, staging, prod, global) | `string` | n/a | yes |
| <a name="input_kubernetes_tags"></a> [kubernetes\_tags](#input\_kubernetes\_tags) | Configuration for Kubernetes integration tags | <pre>object({<br/>    enabled                    = bool<br/>    cluster_name               = optional(string, "") # Will default to {env}-{deployment_name} if empty<br/>    enable_karpenter_discovery = optional(bool, false)<br/>  })</pre> | <pre>{<br/>  "enable_karpenter_discovery": false,<br/>  "enabled": false<br/>}</pre> | no |
| <a name="input_map_public_ip"></a> [map\_public\_ip](#input\_map\_public\_ip) | Map public IP addresses to new instances. | `bool` | `true` | no |
| <a name="input_pod_subnet_newbits"></a> [pod\_subnet\_newbits](#input\_pod\_subnet\_newbits) | Number of bits to add to the secondary CIDR for pod subnets (e.g., 4 for /20 subnets from /16) | `number` | `4` | no |
| <a name="input_secondary_cidr_block"></a> [secondary\_cidr\_block](#input\_secondary\_cidr\_block) | Secondary CIDR block for pod networking (e.g., 100.64.0.0/16). Uses CG-NAT space to avoid conflicts. | `string` | `""` | no |
| <a name="input_vpc_cidr_block"></a> [vpc\_cidr\_block](#input\_vpc\_cidr\_block) | Top-level CIDR block for the VPC | `string` | `"10.0.0.0/16"` | no |
| <a name="input_vpc_endpoints"></a> [vpc\_endpoints](#input\_vpc\_endpoints) | Map of VPC endpoint configurations | <pre>map(object({<br/>    service     = string<br/>    type        = string<br/>    private_dns = optional(bool, false) # Make private_dns optional with default false<br/>  }))</pre> | <pre>{<br/>  "cloudwatch": {<br/>    "private_dns": true,<br/>    "service": "logs",<br/>    "type": "Interface"<br/>  },<br/>  "ecr_api": {<br/>    "private_dns": true,<br/>    "service": "ecr.api",<br/>    "type": "Interface"<br/>  },<br/>  "ecr_dkr": {<br/>    "private_dns": true,<br/>    "service": "ecr.dkr",<br/>    "type": "Interface"<br/>  },<br/>  "s3": {<br/>    "service": "s3",<br/>    "type": "Gateway"<br/>  },<br/>  "secretsmanager": {<br/>    "private_dns": true,<br/>    "service": "secretsmanager",<br/>    "type": "Interface"<br/>  },<br/>  "sns": {<br/>    "private_dns": false,<br/>    "service": "sns",<br/>    "type": "Interface"<br/>  }<br/>}</pre> | no |
| <a name="input_vpce_security_group_rules"></a> [vpce\_security\_group\_rules](#input\_vpce\_security\_group\_rules) | Security group rules for VPC endpoints | <pre>object({<br/>    ingress_security_group_ids = optional(list(string), [])<br/>    ingress_cidr_blocks        = optional(list(string), [])<br/>    egress_cidr_blocks         = optional(list(string), ["0.0.0.0/0"])<br/>  })</pre> | <pre>{<br/>  "egress_cidr_blocks": [<br/>    "0.0.0.0/0"<br/>  ],<br/>  "ingress_cidr_blocks": [],<br/>  "ingress_security_group_ids": []<br/>}</pre> | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_nat_eip"></a> [nat\_eip](#output\_nat\_eip) | The public IP of the NAT gateway |
| <a name="output_pod_subnet_cidrs"></a> [pod\_subnet\_cidrs](#output\_pod\_subnet\_cidrs) | The CIDR blocks of the pod subnets |
| <a name="output_pod_subnet_ids"></a> [pod\_subnet\_ids](#output\_pod\_subnet\_ids) | The IDs of the pod subnets (from secondary CIDR) |
| <a name="output_pod_subnets_by_az"></a> [pod\_subnets\_by\_az](#output\_pod\_subnets\_by\_az) | Map of pod subnets by availability zone |
| <a name="output_private_route_table_id"></a> [private\_route\_table\_id](#output\_private\_route\_table\_id) | The ID of the private route table |
| <a name="output_private_subnet_cidrs"></a> [private\_subnet\_cidrs](#output\_private\_subnet\_cidrs) | The CIDR blocks of the private subnets |
| <a name="output_private_subnet_ids"></a> [private\_subnet\_ids](#output\_private\_subnet\_ids) | The IDs of the private subnets |
| <a name="output_public_subnet_cidrs"></a> [public\_subnet\_cidrs](#output\_public\_subnet\_cidrs) | The CIDR blocks of the public subnets |
| <a name="output_public_subnet_ids"></a> [public\_subnet\_ids](#output\_public\_subnet\_ids) | The IDs of the public subnets |
| <a name="output_secondary_cidr_block"></a> [secondary\_cidr\_block](#output\_secondary\_cidr\_block) | The secondary CIDR block for pod networking |
| <a name="output_vpc_cidr"></a> [vpc\_cidr](#output\_vpc\_cidr) | The CIDR block used by the VPC |
| <a name="output_vpc_endpoints"></a> [vpc\_endpoints](#output\_vpc\_endpoints) | Map of VPC endpoint configurations |
| <a name="output_vpc_id"></a> [vpc\_id](#output\_vpc\_id) | The ID of the VPC |
| <a name="output_vpce_security_group"></a> [vpce\_security\_group](#output\_vpce\_security\_group) | Security group for VPC endpoints |
<!-- END_TF_DOCS -->
<!-- markdownlint-restore -->

---

## Development

### Prerequisites

- [Terraform](https://www.terraform.io/downloads.html)
- [pre-commit](https://pre-commit.com/#install)
- [Terratest](https://terratest.gruntwork.io/docs/getting-started/install/)
- [terraform-docs](https://github.com/terraform-docs/terraform-docs)
  (used by pre-commit hook)

### Testing the Module

To test the module without destroying the created test infrastructure, run the
following commands:

```bash
export TASK_X_REMOTE_TASKFILES=1 && \
task terraform:run-terratest -y DESTROY=false
```

To do a complete testing run, including destroying the test infrastructure, run:

```bash
export TASK_X_REMOTE_TASKFILES=1 && \
task terraform:run-terratest -y
```

### Pre-Commit Hooks

Install, update, and run pre-commit hooks:

```bash
export TASK_X_REMOTE_TASKFILES=1 && \
task run-pre-commit -y
```
