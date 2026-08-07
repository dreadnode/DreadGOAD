locals {
  create_asg      = var.enable_asg
  is_windows      = var.os_type == "windows"
  is_macos        = var.os_type == "macos"
  is_linux        = var.os_type == "linux"
  create_key_pair = var.ssh_public_key != "" && !var.enable_ssm

  deployment_name = "${var.env}-${var.instance_name}"

  kms_key_id = var.encrypt_volumes && var.kms_key_arn != "" ? var.kms_key_arn : null

  common_tags = merge(
    var.tags,
    {
      Environment  = var.env
      ManagedBy    = "Terraform"
      AccessMethod = var.enable_ssm ? "SSM" : "SSH"
    }
  )
}

resource "aws_instance" "this" {
  #checkov:skip=CKV_AWS_8:Encryption is controlled by var.encrypt_volumes variable
  count = local.create_asg ? 0 : 1

  ami = local.is_windows && length(data.aws_ami.windows) > 0 ? data.aws_ami.windows[0].id : (
    local.is_macos && length(data.aws_ami.macos) > 0 ? data.aws_ami.macos[0].id : (
      local.is_linux && length(data.aws_ami.linux) > 0 ? data.aws_ami.linux[0].id : null
    )
  )
  instance_type               = var.instance_type
  key_name                    = local.create_key_pair ? aws_key_pair.this[0].key_name : null
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = concat([aws_security_group.this.id], var.additional_security_group_ids)
  iam_instance_profile        = var.enable_ssm && var.instance_profile == "" ? aws_iam_instance_profile.ssm[0].name : var.instance_profile
  ebs_optimized               = var.ebs_optimized
  monitoring                  = var.enable_monitoring
  user_data                   = var.user_data != "" ? var.user_data : null
  user_data_replace_on_change = true
  associate_public_ip_address = var.assign_public_ip && !var.enable_ssm
  source_dest_check           = var.source_dest_check

  root_block_device {
    delete_on_termination = var.delete_on_termination
    encrypted             = var.encrypt_volumes
    volume_size           = var.root_volume_size
    volume_type           = var.volume_type
    kms_key_id            = local.kms_key_id
  }

  dynamic "ebs_block_device" {
    for_each = var.additional_ebs_volumes
    content {
      device_name           = ebs_block_device.value.device_name
      volume_size           = ebs_block_device.value.volume_size
      volume_type           = ebs_block_device.value.volume_type
      encrypted             = var.encrypt_volumes
      kms_key_id            = local.kms_key_id
      delete_on_termination = ebs_block_device.value.delete_on_termination
    }
  }

  metadata_options {
    http_endpoint = var.enable_metadata ? "enabled" : "disabled"
    http_tokens   = var.require_imdsv2 ? "required" : "optional"
  }

  lifecycle {
    create_before_destroy = true
  }

  tags = merge(
    local.common_tags,
    {
      Name = local.deployment_name
    }
  )
}

resource "aws_launch_template" "this" {
  count = local.create_asg ? 1 : 0

  name_prefix = "${local.deployment_name}-template-"
  image_id = local.is_windows && length(data.aws_ami.windows) > 0 ? data.aws_ami.windows[0].id : (
    local.is_macos && length(data.aws_ami.macos) > 0 ? data.aws_ami.macos[0].id : (
      local.is_linux && length(data.aws_ami.linux) > 0 ? data.aws_ami.linux[0].id : null
    )
  )
  instance_type = var.instance_type
  key_name      = local.create_key_pair ? aws_key_pair.this[0].key_name : null
  user_data     = var.user_data != "" ? base64encode(var.user_data) : null
  ebs_optimized = var.ebs_optimized

  network_interfaces {
    associate_public_ip_address = var.assign_public_ip
    security_groups             = concat([aws_security_group.this.id], var.additional_security_group_ids)
    delete_on_termination       = true
  }

  iam_instance_profile {
    name = var.enable_ssm && var.instance_profile == "" ? aws_iam_instance_profile.ssm[0].name : var.instance_profile
  }

  monitoring {
    enabled = var.enable_monitoring
  }

  metadata_options {
    http_endpoint               = var.enable_metadata ? "enabled" : "disabled"
    http_tokens                 = var.require_imdsv2 ? "required" : "optional"
    http_put_response_hop_limit = var.metadata_hop_limit
  }

  block_device_mappings {
    device_name = "/dev/sda1"
    ebs {
      delete_on_termination = var.delete_on_termination
      encrypted             = var.encrypt_volumes
      volume_size           = var.root_volume_size
      volume_type           = var.volume_type
      kms_key_id            = local.kms_key_id
    }
  }

  dynamic "block_device_mappings" {
    for_each = var.additional_ebs_volumes
    content {
      device_name = block_device_mappings.value.device_name
      ebs {
        delete_on_termination = block_device_mappings.value.delete_on_termination
        encrypted             = var.encrypt_volumes
        volume_size           = block_device_mappings.value.volume_size
        volume_type           = block_device_mappings.value.volume_type
        kms_key_id            = local.kms_key_id
      }
    }
  }

  tag_specifications {
    resource_type = "instance"
    tags = merge(
      local.common_tags,
      {
        Name = local.deployment_name
      }
    )
  }

  tag_specifications {
    resource_type = "volume"
    tags          = local.common_tags
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_autoscaling_group" "this" {
  count = local.create_asg ? 1 : 0

  name_prefix               = "${local.deployment_name}-asg-"
  min_size                  = var.asg_min_size
  max_size                  = var.asg_max_size
  desired_capacity          = var.asg_desired_capacity
  vpc_zone_identifier       = length(var.asg_subnet_ids) > 0 ? var.asg_subnet_ids : [var.subnet_id]
  health_check_type         = var.asg_health_check_type
  health_check_grace_period = var.asg_health_check_grace_period
  force_delete              = var.asg_force_delete
  termination_policies      = var.asg_termination_policies
  suspended_processes       = var.asg_suspended_processes

  # Updated to include both ALB and NLB target groups
  target_group_arns = concat(
    local.create_alb ? [for tg in aws_lb_target_group.this : tg.arn] : [],
    local.create_nlb ? [for tg in aws_lb_target_group.nlb : tg.arn] : [],
    var.target_group_arns
  )

  launch_template {
    id      = aws_launch_template.this[0].id
    version = "$Latest"
  }

  dynamic "tag" {
    for_each = merge(
      local.common_tags,
      var.asg_tags,
      {
        Name = local.deployment_name
      }
    )
    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = true
    }
  }

  lifecycle {
    create_before_destroy = true

    replace_triggered_by = [
      aws_launch_template.this[0].latest_version
    ]
  }

  depends_on = [
    aws_launch_template.this,
    aws_security_group.this,
  ]
}
