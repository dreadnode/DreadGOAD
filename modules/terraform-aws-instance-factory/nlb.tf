locals {
  create_nlb = var.create_nlb && var.enable_asg

  # Only truncate if deployment_name exceeds the limit for NLB name
  nlb_name = length(local.deployment_name) > 32 ? substr(local.deployment_name, 0, 32) : local.deployment_name

  nlb_tg_name_prefix = local.deployment_name
  get_nlb_tg_name = { for k, v in var.nlb_target_groups : k =>
    length("${local.nlb_tg_name_prefix}-${k}") > 32 ?
    "${trimsuffix(substr(local.nlb_tg_name_prefix, 0, 32 - length(k) - 1), "-")}-${k}" :
    "${trimsuffix(local.nlb_tg_name_prefix, "-")}-${k}"
  }
}

resource "aws_lb" "nlb" {
  # checkov:skip=CKV_AWS_150: Deletion protection is conditionally enabled based on environment
  count = local.create_nlb ? 1 : 0

  name               = local.nlb_name
  internal           = var.nlb_internal
  load_balancer_type = "network"
  subnets            = var.nlb_subnet_ids

  enable_deletion_protection       = var.env == "prod"
  enable_cross_zone_load_balancing = var.nlb_cross_zone_enabled

  dynamic "access_logs" {
    for_each = var.enable_nlb_access_logs && var.access_logs_bucket != null ? [1] : []
    content {
      bucket  = var.access_logs_bucket
      enabled = true
      prefix  = var.nlb_access_logs_prefix
    }
  }

  tags = merge(
    local.common_tags,
    {
      Name = local.deployment_name
    }
  )
}

resource "aws_lb_target_group" "nlb" {
  for_each             = var.nlb_target_groups
  name                 = local.get_nlb_tg_name[each.key]
  port                 = each.value.port
  protocol             = each.value.protocol
  vpc_id               = var.vpc_id
  target_type          = each.value.target_type
  deregistration_delay = var.deregistration_delay
  preserve_client_ip   = each.value.preserve_client_ip

  dynamic "health_check" {
    for_each = [each.value.health_check]
    content {
      enabled             = true
      port                = health_check.value.port
      protocol            = health_check.value.protocol
      path                = health_check.value.protocol == "HTTP" || health_check.value.protocol == "HTTPS" ? health_check.value.path : null
      healthy_threshold   = health_check.value.healthy_threshold
      unhealthy_threshold = health_check.value.unhealthy_threshold
      timeout             = health_check.value.timeout
      interval            = health_check.value.interval
      matcher             = health_check.value.protocol == "HTTP" || health_check.value.protocol == "HTTPS" ? health_check.value.matcher : null
    }
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.deployment_name}-${each.key}-tg"
    }
  )
}

resource "aws_lb_listener" "nlb" {
  # checkov:skip=CKV_AWS_2: "NLB uses TCP protocol, not HTTP/HTTPS"
  # checkov:skip=CKV_AWS_103: "TLS policy not applicable for TCP listeners"
  for_each = var.nlb_listeners

  load_balancer_arn = aws_lb.nlb[0].arn
  port              = each.value.port
  protocol          = each.value.protocol
  certificate_arn   = each.value.certificate_arn
  alpn_policy       = each.value.alpn_policy
  ssl_policy        = each.value.ssl_policy

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.nlb[each.value.target_group_key].arn
  }

  tags = merge(
    local.common_tags,
    {
      Name = "${local.deployment_name}-nlb-listener-${each.key}"
    }
  )
}
