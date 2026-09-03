resource "terraform_data" "key_directory" {
  triggers_replace = dirname(var.private_key_path)

  provisioner "local-exec" {
    command = "mkdir -p -- ${jsonencode(dirname(var.private_key_path))}"
  }
}

resource "tls_private_key" "this" {
  algorithm = "ED25519"
}

resource "local_sensitive_file" "private_key" {
  content         = tls_private_key.this.private_key_openssh
  filename        = var.private_key_path
  file_permission = "0600"

  depends_on = [terraform_data.key_directory]
}

resource "local_file" "public_key" {
  content         = tls_private_key.this.public_key_openssh
  filename        = "${var.private_key_path}.pub"
  file_permission = "0644"

  depends_on = [terraform_data.key_directory]
}
