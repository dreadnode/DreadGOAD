terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.39.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.8.0"
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.5.0"
    }
  }

  required_version = "~> 1.7"
}
