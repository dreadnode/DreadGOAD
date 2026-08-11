# AWS Kali attack box

Deploys an optional Kali Linux attack box into a private DreadGOAD subnet. The
module uses the official Kali AWS Marketplace image family constrained to
Kali's Marketplace product ID, attaches an
`AmazonSSMManagedInstanceCore` instance profile, and installs the SSM agent and
the tools used by live scoring during first boot.

The instance is tagged `Role=AttackBox`, `Project=DreadGOAD`, and
`Environment=<env>` so the CLI can discover it without adding it to the Ansible
inventory.

The instance remains private and is managed through AWS Systems Manager; the
module creates no public ingress rule.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | ~> 6.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_aws"></a> [aws](#provider\_aws) | ~> 6.0 |

## Modules

| Name | Source | Version |
| ---- | ------ | ------- |
| <a name="module_kali"></a> [kali](#module\_kali) | ../terraform-aws-instance-factory | n/a |

## Resources

| Name | Type |
| ---- | ---- |
| [aws_region.current](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/region) | data source |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Additional tags to merge into attack-box resources. | `map(string)` | `{}` | no |
| <a name="input_ami_id"></a> [ami\_id](#input\_ami\_id) | Optional explicit Kali AMI ID override. | `string` | `""` | no |
| <a name="input_ami_name_pattern"></a> [ami\_name\_pattern](#input\_ami\_name\_pattern) | Official Kali Marketplace AMI pattern, constrained to Kali product ID 804fcc46-63fc-4eb6-85a1-50e66d6c7215. Ignored when ami\_id is set. | `string` | `"*kali-last-snapshot-amd64-*-804fcc46-63fc-4eb6-85a1-50e66d6c7215"` | no |
| <a name="input_ami_owners"></a> [ami\_owners](#input\_ami\_owners) | Allowed AMI owners. The default is further constrained by ami\_name\_pattern to the official Kali Marketplace product ID. | `list(string)` | <pre>[<br/>  "aws-marketplace"<br/>]</pre> | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Name of the deployment (for example, goad). | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name (for example, test or staging). | `string` | n/a | yes |
| <a name="input_instance_type"></a> [instance\_type](#input\_instance\_type) | EC2 instance type for the Kali attack box. | `string` | `"t3.medium"` | no |
| <a name="input_root_volume_size"></a> [root\_volume\_size](#input\_root\_volume\_size) | Kali root volume size in GiB. | `number` | `80` | no |
| <a name="input_subnet_id"></a> [subnet\_id](#input\_subnet\_id) | Private subnet in which to deploy the attack box. | `string` | n/a | yes |
| <a name="input_vpc_cidr"></a> [vpc\_cidr](#input\_vpc\_cidr) | Lab VPC CIDR. Traffic from this range may reach the attack box. | `string` | n/a | yes |
| <a name="input_vpc_id"></a> [vpc\_id](#input\_vpc\_id) | VPC in which to deploy the attack box. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_ami_id"></a> [ami\_id](#output\_ami\_id) | Kali AMI selected for the attack box. |
| <a name="output_instance_id"></a> [instance\_id](#output\_instance\_id) | EC2 instance ID of the Kali attack box. |
| <a name="output_private_ip"></a> [private\_ip](#output\_private\_ip) | Private IPv4 address of the Kali attack box. |
| <a name="output_security_group_id"></a> [security\_group\_id](#output\_security\_group\_id) | Security group attached to the Kali attack box. |
<!-- END_TF_DOCS -->
