terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }

  # Configure remote state before first apply:
  # backend "s3" {
  #   bucket         = "your-terraform-state-bucket"
  #   key            = "ngx/terraform.tfstate"
  #   region         = "us-east-1"
  #   dynamodb_table = "terraform-state-lock"
  #   encrypt        = true
  # }
}

provider "aws" {
  region  = var.aws_region
  profile = "nyk-tf"

  default_tags {
    tags = local.common_tags
  }
}

# Stub Lambda deployment package.
# Replaced by real Go builds via: make build-lambdas
data "archive_file" "lambda_stub" {
  type        = "zip"
  source_dir  = "${path.module}/lambda_stub"
  output_path = "${path.module}/.build/lambda_stub.zip"
}
