terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.45.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9.0"
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.6.0"
    }
  }

  required_version = "~> 1.7"
}
