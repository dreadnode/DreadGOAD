variable "private_key_path" {
  description = "Absolute path where the generated private key is written."
  type        = string

  validation {
    condition     = startswith(var.private_key_path, "/")
    error_message = "private_key_path must be absolute."
  }
}
