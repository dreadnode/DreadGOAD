locals {
  create_alb = var.create_alb && var.enable_asg

  # Only truncate if deployment_name exceeds the limit
  lb_name = length(local.deployment_name) > 32 ? substr(local.deployment_name, 0, 32) : local.deployment_name

  tg_name_prefix = local.deployment_name
  get_tg_name = { for k, v in var.target_groups : k =>
    length("${local.tg_name_prefix}${k}") > 32 ?
    "${trimsuffix(substr(local.tg_name_prefix, 0, 32 - length(k) - 1), "-")}-${k}" :
    "${trimsuffix(local.tg_name_prefix, "-")}-${k}"
  }
}

resource "aws_lb" "this" {
  # checkov:skip=CKV2_AWS_28: Ensure public facing ALB are protected by WAF
  # checkov:skip=CKV_AWS_150: Deletion protection is conditionally enabled based on environment
  count = local.create_alb ? 1 : 0

  name               = local.lb_name
  internal           = var.alb_internal
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb[0].id]
  subnets            = var.alb_subnet_ids

  enable_deletion_protection = var.env == "prod"
  drop_invalid_header_fields = var.drop_invalid_header_fields
  idle_timeout               = var.alb_idle_timeout

  dynamic "access_logs" {
    for_each = var.enable_access_logs && var.access_logs_bucket != null ? [1] : []
    content {
      bucket  = var.access_logs_bucket
      enabled = true
    }
  }

  tags = local.common_tags
}

resource "aws_lb_target_group" "this" {
  for_each             = var.target_groups
  name                 = local.get_tg_name[each.key]
  port                 = each.value.port
  protocol             = each.value.protocol
  vpc_id               = var.vpc_id
  target_type          = each.value.target_type
  deregistration_delay = var.deregistration_delay

  health_check {
    enabled             = true
    path                = each.value.health_check_path
    port                = each.value.port
    protocol            = each.value.protocol
    healthy_threshold   = var.health_check_healthy_threshold
    unhealthy_threshold = var.health_check_unhealthy_threshold
    timeout             = var.health_check_timeout
    interval            = var.health_check_interval
    matcher             = "200-499"
  }

  stickiness {
    type            = "lb_cookie"
    enabled         = each.value.stickiness_enabled
    cookie_duration = var.stickiness_cookie_duration
  }

  tags = local.common_tags
}

resource "aws_lb_listener" "https" {
  count = local.create_alb && var.tls_configuration != null ? 1 : 0

  load_balancer_arn = aws_lb.this[0].arn
  port              = 443
  protocol          = "HTTPS"
  ssl_policy        = var.tls_configuration.ssl_policy
  certificate_arn   = var.tls_configuration.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = values(aws_lb_target_group.this)[0].arn
  }
}

resource "aws_lb_listener" "http" {
  count = local.create_alb ? 1 : 0

  load_balancer_arn = aws_lb.this[0].arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = var.tls_configuration != null ? "redirect" : "forward"

    dynamic "redirect" {
      for_each = var.tls_configuration != null ? [1] : []
      content {
        port        = "443"
        protocol    = "HTTPS"
        status_code = "HTTP_301"
      }
    }

    dynamic "forward" {
      for_each = var.tls_configuration == null ? [1] : []
      content {
        target_group {
          arn = one(values(aws_lb_target_group.this)).arn
        }
      }
    }
  }
}
