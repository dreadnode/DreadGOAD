# terraform-aws-linux-instance

Creates one private Ubuntu Linux EC2 instance for service ranges. The module
uses SSM rather than inbound SSH, requires IMDSv2, encrypts all EBS volumes,
and allows traffic only from inside the supplied VPC.

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_aws"></a> [aws](#requirement\_aws) | >= 5.0 |

## Providers

No providers.

## Modules

| Name | Source | Version |
| ---- | ------ | ------- |
| <a name="module_instance"></a> [instance](#module\_instance) | ../terraform-aws-instance-factory | n/a |

## Resources

No resources.

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_additional_tags"></a> [additional\_tags](#input\_additional\_tags) | Additional metadata applied to the instance and its resources. | `map(string)` | `{}` | no |
| <a name="input_data_volume_size"></a> [data\_volume\_size](#input\_data\_volume\_size) | Encrypted range data volume size in GiB; zero disables it. | `number` | `100` | no |
| <a name="input_deployment_name"></a> [deployment\_name](#input\_deployment\_name) | Deployment component used in resource names. | `string` | n/a | yes |
| <a name="input_env"></a> [env](#input\_env) | Environment name. | `string` | n/a | yes |
| <a name="input_instance_name"></a> [instance\_name](#input\_instance\_name) | Logical Linux host name. | `string` | n/a | yes |
| <a name="input_instance_type"></a> [instance\_type](#input\_instance\_type) | EC2 instance type. | `string` | `"m6i.xlarge"` | no |
| <a name="input_private_ip"></a> [private\_ip](#input\_private\_ip) | Fixed private IPv4 address for the instance. | `string` | n/a | yes |
| <a name="input_root_volume_size"></a> [root\_volume\_size](#input\_root\_volume\_size) | Encrypted root volume size in GiB. | `number` | `64` | no |
| <a name="input_subnet_id"></a> [subnet\_id](#input\_subnet\_id) | Private subnet in which to create the instance. | `string` | n/a | yes |
| <a name="input_user_data"></a> [user\_data](#input\_user\_data) | Cloud-init content used to establish the hostname and SSM agent. | `string` | `""` | no |
| <a name="input_vpc_cidr"></a> [vpc\_cidr](#input\_vpc\_cidr) | VPC CIDR allowed to reach the range host. | `string` | n/a | yes |
| <a name="input_vpc_id"></a> [vpc\_id](#input\_vpc\_id) | VPC in which to create the instance. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_ami_id"></a> [ami\_id](#output\_ami\_id) | Ubuntu AMI selected for the instance. |
| <a name="output_instance_id"></a> [instance\_id](#output\_instance\_id) | EC2 instance ID. |
| <a name="output_private_ip"></a> [private\_ip](#output\_private\_ip) | Fixed private IPv4 address. |
| <a name="output_security_group_id"></a> [security\_group\_id](#output\_security\_group\_id) | Security group attached to the instance. |
<!-- END_TF_DOCS -->
