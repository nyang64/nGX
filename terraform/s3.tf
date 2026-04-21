# ── S3 Buckets ────────────────────────────────────────────────────────────────

resource "aws_s3_bucket" "emails" {
  bucket        = local.s3_names.emails
  force_destroy = false
}

resource "aws_s3_bucket" "attachments" {
  bucket        = local.s3_names.attachments
  force_destroy = false
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = local.s3_names.artifacts
  force_destroy = false
}

# ── Versioning ────────────────────────────────────────────────────────────────

resource "aws_s3_bucket_versioning" "emails" {
  bucket = aws_s3_bucket.emails.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_versioning" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  versioning_configuration {
    status = "Enabled"
  }
}

# ── Server-Side Encryption ────────────────────────────────────────────────────

resource "aws_s3_bucket_server_side_encryption_configuration" "emails" {
  bucket = aws_s3_bucket.emails.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "attachments" {
  bucket = aws_s3_bucket.attachments.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# ── Public Access Block ───────────────────────────────────────────────────────

resource "aws_s3_bucket_public_access_block" "emails" {
  bucket = aws_s3_bucket.emails.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "attachments" {
  bucket = aws_s3_bucket.attachments.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_public_access_block" "artifacts" {
  bucket = aws_s3_bucket.artifacts.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ── Lifecycle: Emails Bucket ──────────────────────────────────────────────────

resource "aws_s3_bucket_lifecycle_configuration" "emails" {
  bucket = aws_s3_bucket.emails.id

  rule {
    id     = "email-retention"
    status = "Enabled"

    filter {}

    transition {
      days          = 90
      storage_class = "GLACIER"
    }

    expiration {
      days = 365
    }
  }
}

# ── S3 Event Notification → SQS email_inbound ─────────────────────────────────
# SES writes raw inbound messages to s3://emails/inbound/raw/<key>.
# S3 notifies the email_inbound SQS queue, which triggers the email-inbound Lambda.

resource "aws_s3_bucket_notification" "emails" {
  bucket = aws_s3_bucket.emails.id

  queue {
    id            = "inbound-raw-created"
    queue_arn     = aws_sqs_queue.email_inbound.arn
    events        = ["s3:ObjectCreated:*"]
    filter_prefix = "inbound/raw/"
  }

  depends_on = [aws_sqs_queue_policy.email_inbound]
}
