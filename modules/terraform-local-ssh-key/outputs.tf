output "private_key_path" {
  description = "Path to the generated private key."
  value       = local_sensitive_file.private_key.filename
}

output "public_key_openssh" {
  description = "Generated OpenSSH public key."
  value       = tls_private_key.this.public_key_openssh
}
