output "alb_arn" {
  description = "ARN of the Application Load Balancer"
  value       = local.create_alb ? aws_lb.this[0].arn : null
}

output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = local.create_alb ? aws_lb.this[0].dns_name : null
}

output "alb_id" {
  description = "ID of the Application Load Balancer"
  value       = local.create_alb ? aws_lb.this[0].id : null
}

output "alb_security_group_id" {
  description = "ID of the ALB security group"
  value       = local.create_alb ? aws_security_group.alb[0].id : null
}

output "alb_zone_id" {
  description = "The canonical hosted zone ID of the ALB"
  value       = local.create_alb ? aws_lb.this[0].zone_id : null
}

output "all_instance_details" {
  description = "Detailed information about all instances (both standalone and ASG)"
  value = {
    deployment_type = local.create_asg ? "asg" : "standalone"

    asg = local.create_asg ? {
      id           = aws_autoscaling_group.this[0].id
      name         = aws_autoscaling_group.this[0].name
      private_ips  = length(data.aws_instances.asg_instances) > 0 ? data.aws_instances.asg_instances[0].private_ips : []
      instance_ids = length(data.aws_instances.asg_instances) > 0 ? data.aws_instances.asg_instances[0].ids : []
    } : null

    standalone_instances = !local.create_asg ? {
      for instance in aws_instance.this : instance.id => {
        name       = instance.tags["Name"]
        public_ip  = instance.public_ip
        private_ip = instance.private_ip
        subnet_id  = instance.subnet_id
        os_type    = local.is_windows ? "windows" : (local.is_macos ? "macos" : "linux")
      }
    } : {}

    load_balancers = {
      alb = local.create_alb ? {
        dns_name = aws_lb.this[0].dns_name
        arn      = aws_lb.this[0].arn
        id       = aws_lb.this[0].id
      } : null

      nlb = local.create_nlb ? {
        dns_name = aws_lb.nlb[0].dns_name
        arn      = aws_lb.nlb[0].arn
        id       = aws_lb.nlb[0].id
      } : null
    }
  }
}

output "ami_id" {
  description = "ID of the AMI used"
  value = local.is_windows && length(data.aws_ami.windows) > 0 ? data.aws_ami.windows[0].id : (
    local.is_macos && length(data.aws_ami.macos) > 0 ? data.aws_ami.macos[0].id : (
      local.is_linux && length(data.aws_ami.linux) > 0 ? data.aws_ami.linux[0].id : null
    )
  )
}

output "asg_arn" {
  description = "ARN of the created Auto Scaling Group"
  value       = local.create_asg ? aws_autoscaling_group.this[0].arn : null
}

output "asg_id" {
  description = "ID of the created Auto Scaling Group"
  value       = local.create_asg ? aws_autoscaling_group.this[0].id : null
}

output "asg_name" {
  description = "Name of the created Auto Scaling Group"
  value       = local.create_asg ? aws_autoscaling_group.this[0].name : null
}

output "instance_arns" {
  description = "ARNs of created instances"
  value       = local.create_asg ? [] : aws_instance.this[*].arn
}

output "instance_details" {
  description = "Map of instance details"
  value = local.create_asg ? {
    asg = {
      id = aws_autoscaling_group.this[0].id
    }
    } : {
    for instance in aws_instance.this : instance.id => {
      name       = instance.tags["Name"]
      public_ip  = instance.public_ip
      private_ip = instance.private_ip
      subnet_id  = instance.subnet_id
      os_type    = local.is_windows ? "windows" : (local.is_macos ? "macos" : "linux")
    }
  }
}

output "instance_ids" {
  description = "IDs of created instances (single instance or ASG instances)"
  value       = local.create_asg ? [] : aws_instance.this[*].id
}

output "instance_private_ips" {
  description = "Private IPs of created instances (single instance or ASG instances)"
  value = local.create_asg ? (
    length(data.aws_instances.asg_instances) > 0 ? data.aws_instances.asg_instances[0].private_ips : []
  ) : aws_instance.this[*].private_ip
}

output "instance_public_ips" {
  description = "Public IPs of created instances"
  value       = local.create_asg ? [] : aws_instance.this[*].public_ip
}

output "instance_primary_eni_ids" {
  description = "Primary ENI IDs of created instances"
  value       = local.create_asg ? [] : aws_instance.this[*].primary_network_interface_id
}

output "launch_template_id" {
  description = "ID of the created Launch Template"
  value       = local.create_asg ? aws_launch_template.this[0].id : null
}

output "launch_template_latest_version" {
  description = "Latest version of the Launch Template"
  value       = local.create_asg ? aws_launch_template.this[0].latest_version : null
}

output "nlb_arn" {
  description = "ARN of the Network Load Balancer"
  value       = var.create_nlb ? aws_lb.nlb[0].arn : null
}

output "nlb_dns_name" {
  description = "DNS name of the Network Load Balancer"
  value       = local.create_nlb ? aws_lb.nlb[0].dns_name : null
}

output "nlb_id" {
  description = "ID of the Network Load Balancer"
  value       = local.create_nlb ? aws_lb.nlb[0].id : null
}

output "nlb_zone_id" {
  description = "The canonical hosted zone ID of the NLB"
  value       = local.create_nlb ? aws_lb.nlb[0].zone_id : null
}

output "nlb_target_group_arns" {
  description = "ARNs of the NLB Target Groups"
  value       = local.create_nlb ? { for k, v in aws_lb_target_group.nlb : k => v.arn } : null
}

output "security_group_arn" {
  description = "ARN of the created security group"
  value       = aws_security_group.this.arn
}

output "security_group_id" {
  description = "ID of the created security group"
  value       = aws_security_group.this.id
}

output "target_group_arns" {
  description = "ARNs of the Target Groups"
  value       = local.create_alb ? { for k, v in aws_lb_target_group.this : k => v.arn } : null
}

output "windows_password_data" {
  description = "Password data for Windows instances (encrypted)"
  value       = local.is_windows && !local.create_asg ? aws_instance.this[*].password_data : []
  sensitive   = true
}
