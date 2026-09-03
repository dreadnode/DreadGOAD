# terraform-local-ssh-key

<!-- BEGIN_TF_DOCS -->
## Requirements

| Name | Version |
| ---- | ------- |
| <a name="requirement_terraform"></a> [terraform](#requirement\_terraform) | >= 1.7 |
| <a name="requirement_local"></a> [local](#requirement\_local) | ~> 2.5 |
| <a name="requirement_tls"></a> [tls](#requirement\_tls) | ~> 4.0 |

## Providers

| Name | Version |
| ---- | ------- |
| <a name="provider_local"></a> [local](#provider\_local) | 2.9.0 |
| <a name="provider_terraform"></a> [terraform](#provider\_terraform) | n/a |
| <a name="provider_tls"></a> [tls](#provider\_tls) | 4.3.0 |

## Modules

No modules.

## Resources

| Name | Type |
| ---- | ---- |
| [local_file.public_key](https://registry.terraform.io/providers/hashicorp/local/latest/docs/resources/file) | resource |
| [local_sensitive_file.private_key](https://registry.terraform.io/providers/hashicorp/local/latest/docs/resources/sensitive_file) | resource |
| [terraform_data.key_directory](https://registry.terraform.io/providers/hashicorp/terraform/latest/docs/resources/data) | resource |
| [tls_private_key.this](https://registry.terraform.io/providers/hashicorp/tls/latest/docs/resources/private_key) | resource |

## Inputs

| Name | Description | Type | Default | Required |
| ---- | ----------- | ---- | ------- | :------: |
| <a name="input_private_key_path"></a> [private\_key\_path](#input\_private\_key\_path) | Absolute path where the generated private key is written. | `string` | n/a | yes |

## Outputs

| Name | Description |
| ---- | ----------- |
| <a name="output_private_key_path"></a> [private\_key\_path](#output\_private\_key\_path) | Path to the generated private key. |
| <a name="output_public_key_openssh"></a> [public\_key\_openssh](#output\_public\_key\_openssh) | Generated OpenSSH public key. |
<!-- END_TF_DOCS -->
