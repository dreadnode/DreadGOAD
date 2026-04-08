# Create IAM role and instance profile for SSM access if enabled
resource "aws_iam_role" "ssm" {
  count = var.enable_ssm && var.instance_profile == "" ? 1 : 0
  name  = "${local.deployment_name}-ssm-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "ec2.amazonaws.com"
        }
      }
    ]
  })

  tags = local.common_tags
}

resource "aws_iam_role_policy_attachment" "ssm" {
  count      = var.enable_ssm && var.instance_profile == "" ? 1 : 0
  role       = aws_iam_role.ssm[0].name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

# Add additional IAM policy attachments to the SSM role
resource "aws_iam_role_policy_attachment" "additional_policies" {
  for_each = var.enable_ssm && var.instance_profile == "" ? var.additional_iam_policies : {}

  role       = aws_iam_role.ssm[0].name
  policy_arn = each.value
}

resource "aws_iam_instance_profile" "ssm" {
  count = var.enable_ssm && var.instance_profile == "" ? 1 : 0
  name  = "${local.deployment_name}-ssm-profile"
  role  = aws_iam_role.ssm[0].name
  tags  = local.common_tags
}
