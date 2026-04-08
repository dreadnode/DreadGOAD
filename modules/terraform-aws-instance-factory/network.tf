resource "aws_key_pair" "this" {
  count      = local.create_key_pair ? 1 : 0
  key_name   = "${local.deployment_name}-key-pair"
  public_key = var.ssh_public_key
  tags       = local.common_tags
}
