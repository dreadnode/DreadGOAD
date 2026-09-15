output "instance_id" {
  description = "EC2 instance ID."
  value       = one(module.instance.instance_ids)
}

output "private_ip" {
  description = "Fixed private IPv4 address."
  value       = one(module.instance.instance_private_ips)
}

output "security_group_id" {
  description = "Security group attached to the instance."
  value       = module.instance.security_group_id
}

output "ami_id" {
  description = "Ubuntu AMI selected for the instance."
  value       = module.instance.ami_id
}
