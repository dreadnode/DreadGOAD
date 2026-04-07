# Get the latest Linux AMI
data "aws_ami" "linux" {
  count       = var.os_type == "linux" ? 1 : 0
  most_recent = true
  owners      = var.linux_ami_owners

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }

  # Apply any name filter from additional_linux_ami_filters first
  dynamic "filter" {
    for_each = var.additional_linux_ami_filters
    content {
      name   = filter.value.name
      values = filter.value.values
    }
  }

  # Only apply default name filter if no name/tag:Name/image-id filter exists in additional_linux_ami_filters
  dynamic "filter" {
    for_each = length([for f in var.additional_linux_ami_filters : f if f.name == "name" || f.name == "image-id" || f.name == "tag:Name"]) > 0 ? [] : [1]
    content {
      name   = "name"
      values = ["${var.linux_os}*${var.linux_os_version}*"]
    }
  }

  # Always apply architecture filter when using default instance types
  dynamic "filter" {
    for_each = var.ami_architecture != "" ? [1] : []
    content {
      name   = "architecture"
      values = [var.ami_architecture]
    }
  }
}

# Get the latest Windows AMI
data "aws_ami" "windows" {
  count       = var.os_type == "windows" ? 1 : 0
  most_recent = true
  owners      = var.windows_ami_owners

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }

  # Apply any name filter from additional_windows_ami_filters first
  dynamic "filter" {
    for_each = var.additional_windows_ami_filters
    content {
      name   = filter.value.name
      values = filter.value.values
    }
  }

  # Only apply default name filter if no name/tag:Name/image-id filter exists in additional_windows_ami_filters
  dynamic "filter" {
    for_each = length([for f in var.additional_windows_ami_filters : f if f.name == "name" || f.name == "image-id" || f.name == "tag:Name"]) > 0 ? [] : [1]
    content {
      name   = "name"
      values = ["${var.windows_os}-${var.windows_os_version}*"]
    }
  }

  # Always apply architecture filter when using default instance types
  dynamic "filter" {
    for_each = var.ami_architecture != "" ? [1] : []
    content {
      name   = "architecture"
      values = [var.ami_architecture]
    }
  }
}

# Get the latest macOS AMI
data "aws_ami" "macos" {
  count       = var.os_type == "macos" ? 1 : 0
  most_recent = true
  owners      = var.macos_ami_owners

  filter {
    name   = "root-device-type"
    values = ["ebs"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }

  # Apply any name filter from additional_macos_ami_filters first
  dynamic "filter" {
    for_each = var.additional_macos_ami_filters
    content {
      name   = filter.value.name
      values = filter.value.values
    }
  }

  # Only apply default name filter if no name filter exists in additional_macos_ami_filters
  dynamic "filter" {
    for_each = length([for f in var.additional_macos_ami_filters : f if f.name == "name" || f.name == "image-id" || f.name == "tag:Name"]) > 0 ? [] : [1]
    content {
      name   = "name"
      values = ["${var.macos_os}*${var.macos_os_version}*"]
    }
  }

  # Always apply architecture filter when using default instance types
  dynamic "filter" {
    for_each = var.ami_architecture != "" ? [1] : []
    content {
      name   = "architecture"
      values = [var.ami_architecture]
    }
  }
}

data "aws_instances" "asg_instances" {
  count = local.create_asg ? 1 : 0

  filter {
    name   = "instance-state-name"
    values = ["running"]
  }

  filter {
    name   = "tag:aws:autoscaling:groupName"
    values = [aws_autoscaling_group.this[0].name]
  }

  depends_on = [aws_autoscaling_group.this]
}

data "http" "current_ip" {
  count = var.create_alb && var.include_current_ip ? 1 : 0
  url   = "https://api.ipify.org?format=text"
}
