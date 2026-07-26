terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "6.56.0"
    }
  }

  required_version = ">= 0.10.0"
}

provider "aws" {
  region = var.region
  profile = "goad"
}
