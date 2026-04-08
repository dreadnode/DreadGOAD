resource "aws_vpc_endpoint" "endpoints" {
  for_each = local.vpc_endpoints

  vpc_id            = aws_vpc.main.id
  service_name      = "com.amazonaws.${data.aws_region.current.id}.${each.value.service}"
  vpc_endpoint_type = each.value.type

  # Set subnet_ids and security_group_ids only for Interface endpoints
  subnet_ids         = each.value.type == "Interface" ? local.unique_az_subnets : null
  security_group_ids = each.value.type == "Interface" ? [aws_security_group.vpce[0].id] : null

  # Only set route table for Gateway endpoints
  route_table_ids     = each.value.type == "Gateway" ? data.aws_route_tables.private.ids : null
  private_dns_enabled = each.value.private_dns

  lifecycle {
    create_before_destroy = true
    ignore_changes        = [tags, tags_all]
  }

  tags = local.tags
}
