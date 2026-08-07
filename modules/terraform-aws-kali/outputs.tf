output "instance_id" {
  description = "EC2 instance ID of the Kali attack box."
  value       = one(module.kali.instance_ids)
}

output "private_ip" {
  description = "Private IPv4 address of the Kali attack box."
  value       = one(module.kali.instance_private_ips)
}

output "security_group_id" {
  description = "Security group attached to the Kali attack box."
  value       = module.kali.security_group_id
}

output "ami_id" {
  description = "Kali AMI selected for the attack box."
  value       = module.kali.ami_id
}
