# Instance Factory Terraform Module

<div align="center">
<img
  src="https://d1lppblt9t2x15.cloudfront.net/logos/5714928f3cdc09503751580cffbe8d02.png"
  alt="Logo"
  align="center"
  width="144px"
  height="144px"
/>

## Terraform module for flexible EC2 instance deployment ☁️

_... supporting Linux, Windows, and macOS with ASG capabilities_ 🚀

</div>

<div align="center">

[![Terratest](https://github.com/dreadnode/terraform-module/actions/workflows/terratest.yaml/badge.svg)](https://github.com/dreadnode/terraform-module/actions/workflows/terratest.yaml)
[![Pre-Commit](https://github.com/dreadnode/terraform-module/actions/workflows/pre-commit.yaml/badge.svg)](https://github.com/dreadnode/terraform-module/actions/workflows/pre-commit.yaml)
[![Renovate](https://github.com/dreadnode/terraform-module/actions/workflows/renovate.yaml/badge.svg)](https://github.com/dreadnode/terraform-module/actions/workflows/renovate.yaml)

</div>

---

## 📖 Overview

This Terraform module provides a flexible way to deploy EC2 instances in AWS,
supporting multiple operating systems (Linux, Windows, and macOS) with the
option to deploy either single instances or Auto Scaling Groups (ASG). The
module includes comprehensive security features, monitoring capabilities, and
storage management.

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

- EC2 instances with support for Linux, Windows, and macOS
- Auto Scaling Groups with customizable scaling policies
- Launch Templates for consistent instance configuration
- Security Groups with customizable rules
- EBS volumes with encryption support
- SSH Key Pairs for secure access
- Instance Metadata Service v2 (IMDSv2) support
- Detailed monitoring and health checks
- Custom tags and naming conventions

---

## Usage

Here are some examples of how to use this module:

### Basic Linux Instance

```hcl
module "linux_instance" {
  source  = "github.com/dreadnode/terraform-aws-instance-factory"
  env           = "dev"
  workload_name = "web-server"
  os_type       = "linux"
  instance_type = "t3.micro"
  vpc_id        = "vpc-1234567890"
  subnet_id     = "subnet-1234567890"

  ingress_rules = [
    {
      description = "Allow HTTP"
      from_port   = 80
      to_port     = 80
      protocol    = "tcp"
      cidr_blocks = ["0.0.0.0/0"]
    }
  ]
}
```

### Windows Instance with Additional EBS Volume

```hcl
module "windows_instance" {
  source  = "github.com/dreadnode/terraform-aws-instance-factory"
  env           = "prod"
  workload_name = "app-server"
  os_type       = "windows"
  instance_type = "t3.large"
  vpc_id        = "vpc-1234567890"
  subnet_id     = "subnet-1234567890"

  additional_ebs_volumes = [
    {
      device_name           = "/dev/xvdf"
      volume_size           = 100
      volume_type          = "gp3"
      delete_on_termination = true
    }
  ]
}
```

### Auto Scaling Group Configuration

```hcl
module "asg_deployment" {
  source  = "github.com/dreadnode/terraform-aws-instance-factory"
  env           = "staging"
  workload_name = "web-cluster"
  os_type       = "linux"
  instance_type = "t3.small"
  vpc_id        = "vpc-1234567890"
  enable_asg    = true

  asg_subnet_ids       = ["subnet-1234", "subnet-5678"]
  asg_min_size         = 2
  asg_max_size         = 4
  asg_desired_capacity = 2
}
```

---

<!-- markdownlint-disable -->
<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | ~> 1.7 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | ~> 6.42.0 |
| <a name="requirement_http"></a> [http](#requirement\_http) | ~> 3.5.0 |
| <a name="requirement_random"></a> [random](#requirement\_random) | ~> 3.8.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_aws"></a> [aws](#provider\_aws) | 6.42.0 |
| <a name="provider_http"></a> [http](#provider\_http) | 3.5.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [aws_autoscaling_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/autoscaling_group) | resource |
| [aws_iam_instance_profile.ssm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_instance_profile) | resource |
| [aws_iam_role.ssm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role) | resource |
| [aws_iam_role_policy_attachment.additional_policies](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy_attachment) | resource |
| [aws_iam_role_policy_attachment.ssm](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy_attachment) | resource |
| [aws_instance.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/instance) | resource |
| [aws_key_pair.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/key_pair) | resource |
| [aws_launch_template.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/launch_template) | resource |
| [aws_lb.nlb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb) | resource |
| [aws_lb.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb) | resource |
| [aws_lb_listener.http](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_listener.https](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_listener.nlb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_listener) | resource |
| [aws_lb_target_group.nlb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group) | resource |
| [aws_lb_target_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lb_target_group) | resource |
| [aws_security_group.alb](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_security_group.this](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/security_group) | resource |
| [aws_ami.linux](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ami) | data source |
| [aws_ami.macos](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ami) | data source |
| [aws_ami.windows](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/ami) | data source |
| [aws_instances.asg_instances](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/instances) | data source |
| [http_http.current_ip](https://registry.terraform.io/providers/hashicorp/http/latest/docs/data-sources/http) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_access_logs_bucket"></a> [access\_logs\_bucket](#input\_access\_logs\_bucket) | S3 bucket for ALB access logs | `string` | `null` | no |
| <a name="input_additional_ebs_volumes"></a> [additional\_ebs\_volumes](#input\_additional\_ebs\_volumes) | Additional EBS volumes to attach | <pre>list(object({<br/>    device_name           = string<br/>    volume_size           = number<br/>    volume_type           = string<br/>    delete_on_termination = bool<br/>  }))</pre> | `[]` | no |
| <a name="input_additional_iam_policies"></a> [additional\_iam\_policies](#input\_additional\_iam\_policies) | Map of additional IAM policies to attach to the instance role (if SSM is enabled). Example usage: additional\_iam\_policies = { s3\_full\_access = "arn:aws:iam::aws:policy/AmazonS3FullAccess" } | `map(string)` | `{}` | no |
| <a name="input_additional_linux_ami_filters"></a> [additional\_linux\_ami\_filters](#input\_additional\_linux\_ami\_filters) | Additional filters for Linux AMI lookup | <pre>list(object({<br/>    name   = string<br/>    values = list(string)<br/>  }))</pre> | `[]` | no |
| <a name="input_additional_macos_ami_filters"></a> [additional\_macos\_ami\_filters](#input\_additional\_macos\_ami\_filters) | Additional filters for macOS AMI lookup | <pre>list(object({<br/>    name   = string<br/>    values = list(string)<br/>  }))</pre> | `[]` | no |
| <a name="input_additional_security_group_ids"></a> [additional\_security\_group\_ids](#input\_additional\_security\_group\_ids) | Additional security group IDs to attach | `list(string)` | `[]` | no |
| <a name="input_additional_windows_ami_filters"></a> [additional\_windows\_ami\_filters](#input\_additional\_windows\_ami\_filters) | Additional filters for Windows AMI lookup | <pre>list(object({<br/>    name   = string<br/>    values = list(string)<br/>  }))</pre> | `[]` | no |
| <a name="input_alb_additional_security_group_rules"></a> [alb\_additional\_security\_group\_rules](#input\_alb\_additional\_security\_group\_rules) | List of additional rules for the ALB security group | `list(any)` | `[]` | no |
| <a name="input_alb_idle_timeout"></a> [alb\_idle\_timeout](#input\_alb\_idle\_timeout) | The time in seconds that the connection is allowed to be idle | `number` | `60` | no |
| <a name="input_alb_internal"></a> [alb\_internal](#input\_alb\_internal) | Whether ALB should be internal | `bool` | `false` | no |
| <a name="input_alb_subnet_ids"></a> [alb\_subnet\_ids](#input\_alb\_subnet\_ids) | List of subnet IDs where the ALB will be deployed | `list(string)` | `[]` | no |
| <a name="input_alb_target_port"></a> [alb\_target\_port](#input\_alb\_target\_port) | Port for ALB target group | `number` | `80` | no |
| <a name="input_allowed_cidr_blocks"></a> [allowed\_cidr\_blocks](#input\_allowed\_cidr\_blocks) | List of CIDR blocks allowed to access the ALB | `list(string)` | `[]` | no |
| <a name="input_ami_architecture"></a> [ami\_architecture](#input\_ami\_architecture) | Architecture for AMI selection (x86\_64, arm64, etc.) | `string` | `"x86_64"` | no |
| <a name="input_asg_desired_capacity"></a> [asg\_desired\_capacity](#input\_asg\_desired\_capacity) | Desired capacity of ASG | `number` | `1` | no |
| <a name="input_asg_force_delete"></a> [asg\_force\_delete](#input\_asg\_force\_delete) | Force delete ASG | `bool` | `false` | no |
| <a name="input_asg_health_check_grace_period"></a> [asg\_health\_check\_grace\_period](#input\_asg\_health\_check\_grace\_period) | Health check grace period for ASG | `number` | `300` | no |
| <a name="input_asg_health_check_type"></a> [asg\_health\_check\_type](#input\_asg\_health\_check\_type) | Health check type for ASG | `string` | `"EC2"` | no |
| <a name="input_asg_max_size"></a> [asg\_max\_size](#input\_asg\_max\_size) | Maximum size of ASG | `number` | `1` | no |
| <a name="input_asg_min_size"></a> [asg\_min\_size](#input\_asg\_min\_size) | Minimum size of ASG | `number` | `1` | no |
| <a name="input_asg_subnet_ids"></a> [asg\_subnet\_ids](#input\_asg\_subnet\_ids) | Subnet IDs for ASG | `list(string)` | `[]` | no |
| <a name="input_asg_suspended_processes"></a> [asg\_suspended\_processes](#input\_asg\_suspended\_processes) | List of processes to suspend for the ASG (e.g., ReplaceUnhealthy, Launch, Terminate) | `list(string)` | `[]` | no |
| <a name="input_asg_tags"></a> [asg\_tags](#input\_asg\_tags) | Additional tags for ASG | `map(string)` | `{}` | no |
| <a name="input_asg_termination_policies"></a> [asg\_termination\_policies](#input\_asg\_termination\_policies) | Termination policies for ASG | `list(string)` | <pre>[<br/>  "Default"<br/>]</pre> | no |
| <a name="input_assign_public_ip"></a> [assign\_public\_ip](#input\_assign\_public\_ip) | Assign public IP address to the instance(s) | `bool` | `false` | no |
| <a name="input_create_alb"></a> [create\_alb](#input\_create\_alb) | Whether to create an Application Load Balancer | `bool` | `false` | no |
| <a name="input_create_nlb"></a> [create\_nlb](#input\_create\_nlb) | Whether to create a Network Load Balancer | `bool` | `false` | no |
| <a name="input_delete_on_termination"></a> [delete\_on\_termination](#input\_delete\_on\_termination) | Delete volumes on instance termination | `bool` | `true` | no |
| <a name="input_deregistration_delay"></a> [deregistration\_delay](#input\_deregistration\_delay) | Amount of time to wait for in-flight requests before deregistering a target | `number` | `300` | no |
| <a name="input_drop_invalid_header_fields"></a> [drop\_invalid\_header\_fields](#input\_drop\_invalid\_header\_fields) | Drop invalid header fields in HTTP(S) requests | `bool` | `true` | no |
| <a name="input_ebs_optimized"></a> [ebs\_optimized](#input\_ebs\_optimized) | Enable EBS optimization | `bool` | `true` | no |
| <a name="input_egress_rules"></a> [egress\_rules](#input\_egress\_rules) | List of egress rules to create | `list(any)` | `[]` | no |
| <a name="input_enable_access_logs"></a> [enable\_access\_logs](#input\_enable\_access\_logs) | Enable ALB access logging to S3 | `bool` | `false` | no |
| <a name="input_enable_asg"></a> [enable\_asg](#input\_enable\_asg) | Whether to create an Auto Scaling Group instead of a single instance | `bool` | `false` | no |
| <a name="input_enable_metadata"></a> [enable\_metadata](#input\_enable\_metadata) | Enable metadata service | `bool` | `true` | no |
| <a name="input_enable_monitoring"></a> [enable\_monitoring](#input\_enable\_monitoring) | Enable detailed monitoring | `bool` | `true` | no |
| <a name="input_enable_nlb_access_logs"></a> [enable\_nlb\_access\_logs](#input\_enable\_nlb\_access\_logs) | Enable NLB access logging to S3 | `bool` | `false` | no |
| <a name="input_enable_ssm"></a> [enable\_ssm](#input\_enable\_ssm) | Enable AWS Systems Manager Session Manager access | `bool` | `false` | no |
| <a name="input_encrypt_volumes"></a> [encrypt\_volumes](#input\_encrypt\_volumes) | Enable volume encryption | `bool` | `true` | no |
| <a name="input_env"></a> [env](#input\_env) | Environment name (e.g., 'dev', 'staging', 'prod') | `string` | n/a | yes |
| <a name="input_health_check_healthy_threshold"></a> [health\_check\_healthy\_threshold](#input\_health\_check\_healthy\_threshold) | Number of consecutive health check successes required | `number` | `2` | no |
| <a name="input_health_check_interval"></a> [health\_check\_interval](#input\_health\_check\_interval) | Health check interval in seconds | `number` | `30` | no |
| <a name="input_health_check_timeout"></a> [health\_check\_timeout](#input\_health\_check\_timeout) | Health check timeout in seconds | `number` | `10` | no |
| <a name="input_health_check_unhealthy_threshold"></a> [health\_check\_unhealthy\_threshold](#input\_health\_check\_unhealthy\_threshold) | Number of consecutive health check failures required | `number` | `5` | no |
| <a name="input_include_current_ip"></a> [include\_current\_ip](#input\_include\_current\_ip) | Whether to include the current IP address in the allowed CIDR blocks | `bool` | `false` | no |
| <a name="input_ingress_rules"></a> [ingress\_rules](#input\_ingress\_rules) | List of ingress rules to create | `list(any)` | `[]` | no |
| <a name="input_instance_name"></a> [instance\_name](#input\_instance\_name) | Name of the instance(s) | `string` | n/a | yes |
| <a name="input_instance_profile"></a> [instance\_profile](#input\_instance\_profile) | IAM instance profile name. If empty and enable\_ssm is true, a new profile will be created | `string` | `""` | no |
| <a name="input_instance_type"></a> [instance\_type](#input\_instance\_type) | EC2 instance type | `string` | n/a | yes |
| <a name="input_kms_key_arn"></a> [kms\_key\_arn](#input\_kms\_key\_arn) | KMS key ARN for volume encryption. If empty and encrypt\_volumes is true, AWS default encryption will be used | `string` | `""` | no |
| <a name="input_linux_ami_owners"></a> [linux\_ami\_owners](#input\_linux\_ami\_owners) | List of Linux AMI owners | `list(string)` | <pre>[<br/>  "amazon"<br/>]</pre> | no |
| <a name="input_linux_os"></a> [linux\_os](#input\_linux\_os) | Linux OS name | `string` | `"ubuntu/images/hvm-ssd/ubuntu-jammy"` | no |
| <a name="input_linux_os_version"></a> [linux\_os\_version](#input\_linux\_os\_version) | Linux OS version pattern | `string` | `"*"` | no |
| <a name="input_macos_ami_owners"></a> [macos\_ami\_owners](#input\_macos\_ami\_owners) | List of macOS AMI owners | `list(string)` | <pre>[<br/>  "amazon"<br/>]</pre> | no |
| <a name="input_macos_os"></a> [macos\_os](#input\_macos\_os) | macOS name | `string` | `"amzn-ec2-macos"` | no |
| <a name="input_macos_os_version"></a> [macos\_os\_version](#input\_macos\_os\_version) | macOS version | `string` | `"13"` | no |
| <a name="input_metadata_hop_limit"></a> [metadata\_hop\_limit](#input\_metadata\_hop\_limit) | Metadata service hop limit | `number` | `1` | no |
| <a name="input_nlb_access_logs_prefix"></a> [nlb\_access\_logs\_prefix](#input\_nlb\_access\_logs\_prefix) | S3 prefix for NLB access logs | `string` | `"nlb-logs"` | no |
| <a name="input_nlb_cross_zone_enabled"></a> [nlb\_cross\_zone\_enabled](#input\_nlb\_cross\_zone\_enabled) | Enable cross-zone load balancing for NLB | `bool` | `true` | no |
| <a name="input_nlb_internal"></a> [nlb\_internal](#input\_nlb\_internal) | Whether NLB should be internal | `bool` | `false` | no |
| <a name="input_nlb_listeners"></a> [nlb\_listeners](#input\_nlb\_listeners) | Map of NLB listener configurations | <pre>map(object({<br/>    port             = number<br/>    protocol         = string<br/>    target_group_key = string<br/>    certificate_arn  = optional(string, null)<br/>    alpn_policy      = optional(string, null)<br/>    ssl_policy       = optional(string, null)<br/>  }))</pre> | `{}` | no |
| <a name="input_nlb_subnet_ids"></a> [nlb\_subnet\_ids](#input\_nlb\_subnet\_ids) | List of subnet IDs where the NLB will be deployed | `list(string)` | `[]` | no |
| <a name="input_nlb_target_groups"></a> [nlb\_target\_groups](#input\_nlb\_target\_groups) | Map of NLB target group configurations | <pre>map(object({<br/>    port               = number<br/>    protocol           = string<br/>    target_type        = string<br/>    preserve_client_ip = optional(bool, true)<br/>    health_check = optional(object({<br/>      port                = optional(string, "traffic-port")<br/>      protocol            = optional(string, "TCP")<br/>      path                = optional(string, "/")<br/>      healthy_threshold   = optional(number, 3)<br/>      unhealthy_threshold = optional(number, 3)<br/>      timeout             = optional(number, 10)<br/>      interval            = optional(number, 30)<br/>      matcher             = optional(string, "200-399")<br/>    }), {})<br/>  }))</pre> | `{}` | no |
| <a name="input_os_type"></a> [os\_type](#input\_os\_type) | Operating system type (linux, windows, or macos) | `string` | n/a | yes |
| <a name="input_require_imdsv2"></a> [require\_imdsv2](#input\_require\_imdsv2) | Require IMDSv2 metadata | `bool` | `true` | no |
| <a name="input_root_volume_size"></a> [root\_volume\_size](#input\_root\_volume\_size) | Size of root volume in GB | `number` | `100` | no |
| <a name="input_source_dest_check"></a> [source\_dest\_check](#input\_source\_dest\_check) | Controls if traffic is routed to the instance when the destination address does not match the instance. Used for NAT or VPN instances. | `bool` | `true` | no |
| <a name="input_ssh_allowed_cidr_blocks"></a> [ssh\_allowed\_cidr\_blocks](#input\_ssh\_allowed\_cidr\_blocks) | CIDR blocks allowed for SSH access when using SSH instead of SSM | `list(string)` | `[]` | no |
| <a name="input_ssh_public_key"></a> [ssh\_public\_key](#input\_ssh\_public\_key) | Public SSH key. Cannot be set if SSM is enabled | `string` | `""` | no |
| <a name="input_stickiness_cookie_duration"></a> [stickiness\_cookie\_duration](#input\_stickiness\_cookie\_duration) | Cookie duration in seconds for session stickiness | `number` | `86400` | no |
| <a name="input_subnet_id"></a> [subnet\_id](#input\_subnet\_id) | Subnet ID for single instance deployment | `string` | `""` | no |
| <a name="input_tags"></a> [tags](#input\_tags) | Additional tags for resources | `map(string)` | `{}` | no |
| <a name="input_target_group_arns"></a> [target\_group\_arns](#input\_target\_group\_arns) | Target group ARNs for ASG | `list(string)` | `[]` | no |
| <a name="input_target_groups"></a> [target\_groups](#input\_target\_groups) | Map of target group configurations | <pre>map(object({<br/>    name               = string<br/>    port               = number<br/>    protocol           = string<br/>    target_type        = string<br/>    health_check_path  = optional(string, "/")<br/>    stickiness_enabled = optional(bool, false)<br/>  }))</pre> | `{}` | no |
| <a name="input_tls_configuration"></a> [tls\_configuration](#input\_tls\_configuration) | TLS configuration for HTTPS listener | <pre>object({<br/>    certificate_arn = string<br/>    ssl_policy      = string<br/>  })</pre> | `null` | no |
| <a name="input_user_data"></a> [user\_data](#input\_user\_data) | User data script | `string` | `""` | no |
| <a name="input_volume_type"></a> [volume\_type](#input\_volume\_type) | EBS volume type | `string` | `"gp3"` | no |
| <a name="input_vpc_id"></a> [vpc\_id](#input\_vpc\_id) | VPC ID where resources will be created | `string` | n/a | yes |
| <a name="input_windows_ami_owners"></a> [windows\_ami\_owners](#input\_windows\_ami\_owners) | List of Windows AMI owners | `list(string)` | <pre>[<br/>  "amazon"<br/>]</pre> | no |
| <a name="input_windows_os"></a> [windows\_os](#input\_windows\_os) | Windows OS name | `string` | `"Windows_Server"` | no |
| <a name="input_windows_os_version"></a> [windows\_os\_version](#input\_windows\_os\_version) | Windows OS version | `string` | `"2022-English-Full-Base"` | no |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_alb_arn"></a> [alb\_arn](#output\_alb\_arn) | ARN of the Application Load Balancer |
| <a name="output_alb_dns_name"></a> [alb\_dns\_name](#output\_alb\_dns\_name) | DNS name of the Application Load Balancer |
| <a name="output_alb_id"></a> [alb\_id](#output\_alb\_id) | ID of the Application Load Balancer |
| <a name="output_alb_security_group_id"></a> [alb\_security\_group\_id](#output\_alb\_security\_group\_id) | ID of the ALB security group |
| <a name="output_alb_zone_id"></a> [alb\_zone\_id](#output\_alb\_zone\_id) | The canonical hosted zone ID of the ALB |
| <a name="output_all_instance_details"></a> [all\_instance\_details](#output\_all\_instance\_details) | Detailed information about all instances (both standalone and ASG) |
| <a name="output_ami_id"></a> [ami\_id](#output\_ami\_id) | ID of the AMI used |
| <a name="output_asg_arn"></a> [asg\_arn](#output\_asg\_arn) | ARN of the created Auto Scaling Group |
| <a name="output_asg_id"></a> [asg\_id](#output\_asg\_id) | ID of the created Auto Scaling Group |
| <a name="output_asg_name"></a> [asg\_name](#output\_asg\_name) | Name of the created Auto Scaling Group |
| <a name="output_instance_arns"></a> [instance\_arns](#output\_instance\_arns) | ARNs of created instances |
| <a name="output_instance_details"></a> [instance\_details](#output\_instance\_details) | Map of instance details |
| <a name="output_instance_ids"></a> [instance\_ids](#output\_instance\_ids) | IDs of created instances (single instance or ASG instances) |
| <a name="output_instance_primary_eni_ids"></a> [instance\_primary\_eni\_ids](#output\_instance\_primary\_eni\_ids) | Primary ENI IDs of created instances |
| <a name="output_instance_private_ips"></a> [instance\_private\_ips](#output\_instance\_private\_ips) | Private IPs of created instances (single instance or ASG instances) |
| <a name="output_instance_public_ips"></a> [instance\_public\_ips](#output\_instance\_public\_ips) | Public IPs of created instances |
| <a name="output_launch_template_id"></a> [launch\_template\_id](#output\_launch\_template\_id) | ID of the created Launch Template |
| <a name="output_launch_template_latest_version"></a> [launch\_template\_latest\_version](#output\_launch\_template\_latest\_version) | Latest version of the Launch Template |
| <a name="output_nlb_arn"></a> [nlb\_arn](#output\_nlb\_arn) | ARN of the Network Load Balancer |
| <a name="output_nlb_dns_name"></a> [nlb\_dns\_name](#output\_nlb\_dns\_name) | DNS name of the Network Load Balancer |
| <a name="output_nlb_id"></a> [nlb\_id](#output\_nlb\_id) | ID of the Network Load Balancer |
| <a name="output_nlb_target_group_arns"></a> [nlb\_target\_group\_arns](#output\_nlb\_target\_group\_arns) | ARNs of the NLB Target Groups |
| <a name="output_nlb_zone_id"></a> [nlb\_zone\_id](#output\_nlb\_zone\_id) | The canonical hosted zone ID of the NLB |
| <a name="output_security_group_arn"></a> [security\_group\_arn](#output\_security\_group\_arn) | ARN of the created security group |
| <a name="output_security_group_id"></a> [security\_group\_id](#output\_security\_group\_id) | ID of the created security group |
| <a name="output_target_group_arns"></a> [target\_group\_arns](#output\_target\_group\_arns) | ARNs of the Target Groups |
| <a name="output_windows_password_data"></a> [windows\_password\_data](#output\_windows\_password\_data) | Password data for Windows instances (encrypted) |
<!-- END_TF_DOCS -->
<!-- markdownlint-restore -->

---

## Development

### Prerequisites

- [Terraform](https://www.terraform.io/downloads.html) (~> 1.7)
- [pre-commit](https://pre-commit.com/#install)
- [Terratest](https://terratest.gruntwork.io/docs/getting-started/install/)
- [terraform-docs](https://github.com/terraform-docs/terraform-docs)

### Testing the Module

To test the module without destroying the created test infrastructure:

```bash
export TASK_X_REMOTE_TASKFILES=1 && \
task terraform:run-terratest -y DESTROY=false
```

To run a complete test including infrastructure cleanup:

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
