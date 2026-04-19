# SES: inbound email receipt — replaces the self-hosted SMTP server.
# SES receives email for var.mail_domain, stores raw RFC 5322 to S3,
# then S3 notifies the email_inbound SQS queue (configured in s3.tf).

# ── Domain Identity ────────────────────────────────────────────────────────────

resource "aws_ses_domain_identity" "main" {
  domain = var.mail_domain
}

resource "aws_ses_domain_dkim" "main" {
  domain = aws_ses_domain_identity.main.domain
}

# ── Receipt Rule Set ───────────────────────────────────────────────────────────

resource "aws_ses_receipt_rule_set" "main" {
  rule_set_name = "${local.prefix}-receipt-rules"
}

resource "aws_ses_active_receipt_rule_set" "main" {
  rule_set_name = aws_ses_receipt_rule_set.main.rule_set_name
}

# ── Receipt Rule: store raw email to S3 ───────────────────────────────────────
# SES writes raw RFC 5322 to s3://emails-bucket/inbound/raw/<messageId>
# The S3 ObjectCreated notification (in s3.tf) then enqueues to email_inbound SQS.

resource "aws_ses_receipt_rule" "store_to_s3" {
  name          = "${local.prefix}-store-to-s3"
  rule_set_name = aws_ses_receipt_rule_set.main.rule_set_name
  recipients    = [var.mail_domain]
  enabled       = true
  scan_enabled  = true # SES spam/virus scanning before delivery
  tls_policy    = "Require"

  s3_action {
    bucket_name       = aws_s3_bucket.emails.id
    object_key_prefix = "inbound/raw/"
    position          = 1
  }

  depends_on = [aws_s3_bucket_policy.ses_write]
}

# ── S3 Bucket Policy: allow SES to write to the emails bucket ─────────────────

data "aws_caller_identity" "current" {}

resource "aws_s3_bucket_policy" "ses_write" {
  bucket = aws_s3_bucket.emails.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSESWrite"
        Effect = "Allow"
        Principal = {
          Service = "ses.amazonaws.com"
        }
        Action   = "s3:PutObject"
        Resource = "${aws_s3_bucket.emails.arn}/inbound/raw/*"
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
        }
      }
    ]
  })
}

# ── Outputs ────────────────────────────────────────────────────────────────────

output "ses_dkim_tokens" {
  description = "DKIM CNAME records to add to DNS for ${var.mail_domain}"
  value       = aws_ses_domain_dkim.main.dkim_tokens
}

output "ses_verification_token" {
  description = "TXT record value for SES domain verification"
  value       = aws_ses_domain_identity.main.verification_token
}
