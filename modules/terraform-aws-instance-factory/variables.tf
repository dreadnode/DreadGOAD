variable "access_logs_bucket" {
  description = "S3 bucket for ALB access logs"
  type        = string
  default     = null
}

variable "additional_ebs_volumes" {
  description = "Additional EBS volumes to attach"
  type = list(object({
    device_name           = string
    volume_size           = number
    volume_type           = string
    delete_on_termination = bool
  }))
  default = []
}

variable "additional_iam_policies" {
  description = "Map of additional IAM policies to attach to the instance role (if SSM is enabled). Example usage: additional_iam_policies = { s3_full_access = \"arn:aws:iam::aws:policy/AmazonS3FullAccess\" }"
  type        = map(string)
  default     = {}
}

variable "additional_linux_ami_filters" {
  description = "Additional filters for Linux AMI lookup"
  type = list(object({
    name   = string
    values = list(string)
  }))
  default = []
}

variable "additional_macos_ami_filters" {
  description = "Additional filters for macOS AMI lookup"
  type = list(object({
    name   = string
    values = list(string)
  }))
  default = []
}

variable "additional_security_group_ids" {
  type        = list(string)
  description = "Additional security group IDs to attach"
  default     = []
}

variable "additional_windows_ami_filters" {
  description = "Additional filters for Windows AMI lookup"
  type = list(object({
    name   = string
    values = list(string)
  }))
  default = []
}

variable "alb_additional_security_group_rules" {
  description = "List of additional rules for the ALB security group"
  type        = list(any)
  default     = []
}

variable "alb_idle_timeout" {
  description = "The time in seconds that the connection is allowed to be idle"
  type        = number
  default     = 60
}

variable "alb_internal" {
  description = "Whether ALB should be internal"
  type        = bool
  default     = false
}

variable "alb_subnet_ids" {
  description = "List of subnet IDs where the ALB will be deployed"
  type        = list(string)
  default     = []
}

variable "alb_target_port" {
  description = "Port for ALB target group"
  type        = number
  default     = 80
}

variable "allowed_cidr_blocks" {
  description = "List of CIDR blocks allowed to access the ALB"
  type        = list(string)
  default     = []
}

variable "ami_architecture" {
  description = "Architecture for AMI selection (x86_64, arm64, etc.)"
  type        = string
  default     = "x86_64"
}

variable "source_dest_check" {
  description = "Controls if traffic is routed to the instance when the destination address does not match the instance. Used for NAT or VPN instances."
  type        = bool
  default     = true
}

variable "assign_public_ip" {
  type        = bool
  description = "Assign public IP address to the instance(s)"
  default     = false
}

variable "asg_desired_capacity" {
  type        = number
  description = "Desired capacity of ASG"
  default     = 1
}

variable "asg_force_delete" {
  type        = bool
  description = "Force delete ASG"
  default     = false
}

variable "asg_health_check_grace_period" {
  type        = number
  description = "Health check grace period for ASG"
  default     = 300
}

variable "asg_health_check_type" {
  type        = string
  description = "Health check type for ASG"
  default     = "EC2"
}

variable "asg_max_size" {
  type        = number
  description = "Maximum size of ASG"
  default     = 1
}

variable "asg_min_size" {
  type        = number
  description = "Minimum size of ASG"
  default     = 1
}

variable "asg_subnet_ids" {
  type        = list(string)
  description = "Subnet IDs for ASG"
  default     = []
}

variable "asg_tags" {
  description = "Additional tags for ASG"
  type        = map(string)
  default     = {}
}

variable "asg_termination_policies" {
  type        = list(string)
  description = "Termination policies for ASG"
  default     = ["Default"]
}

variable "asg_suspended_processes" {
  type        = list(string)
  description = "List of processes to suspend for the ASG (e.g., ReplaceUnhealthy, Launch, Terminate)"
  default     = []
}

variable "create_alb" {
  description = "Whether to create an Application Load Balancer"
  type        = bool
  default     = false
}

variable "create_nlb" {
  description = "Whether to create a Network Load Balancer"
  type        = bool
  default     = false
}

variable "delete_on_termination" {
  type        = bool
  description = "Delete volumes on instance termination"
  default     = true
}

variable "deregistration_delay" {
  description = "Amount of time to wait for in-flight requests before deregistering a target"
  type        = number
  default     = 300
}

variable "drop_invalid_header_fields" {
  description = "Drop invalid header fields in HTTP(S) requests"
  type        = bool
  default     = true
}

variable "ebs_optimized" {
  type        = bool
  description = "Enable EBS optimization"
  default     = true
}

variable "egress_rules" {
  description = "List of egress rules to create"
  type        = list(any)
  default     = []
}

variable "enable_access_logs" {
  description = "Enable ALB access logging to S3"
  type        = bool
  default     = false
}

variable "enable_asg" {
  description = "Whether to create an Auto Scaling Group instead of a single instance"
  type        = bool
  default     = false
}

variable "enable_metadata" {
  type        = bool
  description = "Enable metadata service"
  default     = true
}

variable "enable_monitoring" {
  type        = bool
  description = "Enable detailed monitoring"
  default     = true
}

variable "enable_ssm" {
  type        = bool
  description = "Enable AWS Systems Manager Session Manager access"
  default     = false
}

variable "encrypt_volumes" {
  type        = bool
  description = "Enable volume encryption"
  default     = true
}

variable "env" {
  type        = string
  description = "Environment name (e.g., 'dev', 'staging', 'prod')"
}

variable "health_check_healthy_threshold" {
  description = "Number of consecutive health check successes required"
  type        = number
  default     = 2
}

variable "health_check_interval" {
  description = "Health check interval in seconds"
  type        = number
  default     = 30
}

variable "health_check_timeout" {
  description = "Health check timeout in seconds"
  type        = number
  default     = 10
}

variable "health_check_unhealthy_threshold" {
  description = "Number of consecutive health check failures required"
  type        = number
  default     = 5
}

variable "include_current_ip" {
  type        = bool
  description = "Whether to include the current IP address in the allowed CIDR blocks"
  default     = false
}

variable "ingress_rules" {
  description = "List of ingress rules to create"
  type        = list(any)
  default     = []
}

variable "instance_name" {
  type        = string
  description = "Name of the instance(s)"
}

variable "instance_profile" {
  type        = string
  description = "IAM instance profile name. If empty and enable_ssm is true, a new profile will be created"
  default     = ""
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
}

variable "kms_key_arn" {
  description = "KMS key ARN for volume encryption. If empty and encrypt_volumes is true, AWS default encryption will be used"
  type        = string
  default     = ""

  validation {
    condition     = var.kms_key_arn == "" || can(regex("^arn:aws:kms:[a-z0-9-]+:[0-9]{12}:key/[a-f0-9-]+$", var.kms_key_arn))
    error_message = "The kms_key_arn must be a valid KMS key ARN or empty string."
  }
}

variable "linux_ami_owners" {
  type        = list(string)
  description = "List of Linux AMI owners"
  default     = ["amazon"]
}

variable "linux_os" {
  type        = string
  description = "Linux OS name"
  default     = "ubuntu/images/hvm-ssd/ubuntu-jammy"
}

variable "linux_os_version" {
  type        = string
  description = "Linux OS version pattern"
  default     = "*"
}

variable "macos_ami_owners" {
  type        = list(string)
  description = "List of macOS AMI owners"
  default     = ["amazon"]
}

variable "macos_os" {
  type        = string
  description = "macOS name"
  default     = "amzn-ec2-macos"
}

variable "macos_os_version" {
  type        = string
  description = "macOS version"
  default     = "13"
}

variable "metadata_hop_limit" {
  type        = number
  description = "Metadata service hop limit"
  default     = 1
}

# NLB-specific variables
variable "nlb_internal" {
  description = "Whether NLB should be internal"
  type        = bool
  default     = false
}

variable "nlb_subnet_ids" {
  description = "List of subnet IDs where the NLB will be deployed"
  type        = list(string)
  default     = []
}

variable "nlb_target_groups" {
  description = "Map of NLB target group configurations"
  type = map(object({
    port               = number
    protocol           = string
    target_type        = string
    preserve_client_ip = optional(bool, true)
    health_check = optional(object({
      port                = optional(string, "traffic-port")
      protocol            = optional(string, "TCP")
      path                = optional(string, "/")
      healthy_threshold   = optional(number, 3)
      unhealthy_threshold = optional(number, 3)
      timeout             = optional(number, 10)
      interval            = optional(number, 30)
      matcher             = optional(string, "200-399")
    }), {})
  }))
  default = {}

  validation {
    condition = alltrue([
      for key in keys(var.nlb_target_groups) : can(regex("^[a-zA-Z0-9-]+$", key))
    ])
    error_message = "Target group keys must contain only alphanumeric characters and hyphens (no underscores). AWS NLB target group names don't allow underscores."
  }
}

variable "nlb_listeners" {
  description = "Map of NLB listener configurations"
  type = map(object({
    port             = number
    protocol         = string
    target_group_key = string
    certificate_arn  = optional(string, null)
    alpn_policy      = optional(string, null)
    ssl_policy       = optional(string, null)
  }))
  default = {}
}

variable "nlb_cross_zone_enabled" {
  description = "Enable cross-zone load balancing for NLB"
  type        = bool
  default     = true
}

variable "enable_nlb_access_logs" {
  description = "Enable NLB access logging to S3"
  type        = bool
  default     = false
}

variable "nlb_access_logs_prefix" {
  description = "S3 prefix for NLB access logs"
  type        = string
  default     = "nlb-logs"
}

variable "os_type" {
  description = "Operating system type (linux, windows, or macos)"
  type        = string
  validation {
    condition     = contains(["linux", "windows", "macos"], var.os_type)
    error_message = "Valid values for os_type are: linux, windows, macos."
  }
}

variable "require_imdsv2" {
  type        = bool
  description = "Require IMDSv2 metadata"
  default     = true
}

variable "root_volume_size" {
  type        = number
  description = "Size of root volume in GB"
  default     = 100
}

variable "ssh_allowed_cidr_blocks" {
  type        = list(string)
  description = "CIDR blocks allowed for SSH access when using SSH instead of SSM"
  default     = []

  validation {
    condition     = length(var.ssh_allowed_cidr_blocks) == 0 || !var.enable_ssm
    error_message = "SSH CIDR blocks should not be specified when using SSM for access."
  }
}

variable "ssh_public_key" {
  type        = string
  description = "Public SSH key. Cannot be set if SSM is enabled"
  default     = ""

  validation {
    condition     = var.ssh_public_key == "" || !var.enable_ssm
    error_message = "SSH access should not be configured when using SSM. Use either SSM or SSH, not both."
  }
}

variable "stickiness_cookie_duration" {
  description = "Cookie duration in seconds for session stickiness"
  type        = number
  default     = 86400
}

variable "subnet_id" {
  description = "Subnet ID for single instance deployment"
  type        = string
  default     = ""
}

variable "tags" {
  description = "Additional tags for resources"
  type        = map(string)
  default     = {}
}

variable "target_group_arns" {
  type        = list(string)
  description = "Target group ARNs for ASG"
  default     = []
}

variable "target_groups" {
  description = "Map of target group configurations"
  type = map(object({
    name               = string
    port               = number
    protocol           = string
    target_type        = string
    health_check_path  = optional(string, "/")
    stickiness_enabled = optional(bool, false)
  }))
  default = {}
}

variable "tls_configuration" {
  description = "TLS configuration for HTTPS listener"
  type = object({
    certificate_arn = string
    ssl_policy      = string
  })
  default = null
}

variable "user_data" {
  description = "User data script"
  type        = string
  default     = ""
}

variable "volume_type" {
  type        = string
  description = "EBS volume type"
  default     = "gp3"
}

variable "vpc_id" {
  description = "VPC ID where resources will be created"
  type        = string
}

variable "windows_ami_owners" {
  type        = list(string)
  description = "List of Windows AMI owners"
  default     = ["amazon"]
}

variable "windows_os" {
  type        = string
  description = "Windows OS name"
  default     = "Windows_Server"
}

variable "windows_os_version" {
  type        = string
  description = "Windows OS version"
  default     = "2022-English-Full-Base"
}
