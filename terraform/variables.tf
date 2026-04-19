variable "app_name" {
  description = "Application name used as resource name prefix"
  type        = string
  default     = "ngx"
}

variable "environment" {
  description = "Deployment environment (e.g. prod, staging)"
  type        = string
  default     = "prod"
}

variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

# ── Networking ─────────────────────────────────────────────────────────────────

variable "vpc_cidr" {
  description = "CIDR block for the VPC"
  type        = string
  default     = "10.0.0.0/16"
}

# ── Aurora Serverless v2 ───────────────────────────────────────────────────────

variable "aurora_min_capacity" {
  description = "Aurora Serverless v2 minimum ACU (0.5 = ~$43/mo floor)"
  type        = number
  default     = 0.5
}

variable "aurora_max_capacity" {
  description = "Aurora Serverless v2 maximum ACU"
  type        = number
  default     = 16
}

variable "db_name" {
  description = "PostgreSQL database name"
  type        = string
  default     = "ngx"
}

variable "db_username" {
  description = "PostgreSQL master username"
  type        = string
  default     = "ngxadmin"
}

# ── Email ──────────────────────────────────────────────────────────────────────

variable "mail_domain" {
  description = "Domain used for receiving inbound email via SES (e.g. mail.example.com)"
  type        = string
}

# ── Webhooks ───────────────────────────────────────────────────────────────────

variable "webhook_max_retries" {
  description = "Maximum webhook delivery retry attempts"
  type        = number
  default     = 8
}

variable "webhook_encryption_key" {
  description = "64-char hex AES-256 key for encrypting webhook auth headers. Empty = disabled"
  type        = string
  default     = ""
  sensitive   = true
}

# ── Embedding / Search ─────────────────────────────────────────────────────────

variable "embedder_url" {
  description = "URL of the embedding service (OpenAI-compatible /v1/embeddings)"
  type        = string
  default     = ""
}

variable "embedder_model" {
  description = "Embedding model name"
  type        = string
  default     = "nomic-embed-text-v1.5"
}

# ── Tags ───────────────────────────────────────────────────────────────────────

variable "tags" {
  description = "Additional tags applied to all resources"
  type        = map(string)
  default     = {}
}
