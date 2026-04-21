# SES handles both inbound and outbound email — replaces the custom MTA entirely.
#
# Inbound:  SES receives → spam/virus scan → S3 store → S3 notification → SQS → Lambda
# Outbound: email_outbound Lambda calls ses:SendRawEmail → SES handles DKIM/SMTP delivery
# Bounces:  SES Configuration Set → SNS ses_events → SQS ses_events → ses_events Lambda

# ── Domain Identity ────────────────────────────────────────────────────────────

resource "aws_ses_domain_identity" "main" {
  domain = var.mail_domain
}

# SES manages DKIM keys and signing — no private key management needed
resource "aws_ses_domain_dkim" "main" {
  domain = aws_ses_domain_identity.main.domain
}

# ── Sending Configuration Set ──────────────────────────────────────────────────
# Attached to all outbound sends via SES_CONFIGURATION_SET env var.
# Captures bounce, complaint, and delivery events.

resource "aws_ses_configuration_set" "main" {
  name = "${local.prefix}-sending"

  delivery_options {
    tls_policy = "Require"
  }

  reputation_metrics_enabled = true
  sending_enabled            = true
}

# Route bounce/complaint/delivery events → SNS ses_events → SQS ses_events → Lambda
resource "aws_ses_event_destination" "bounces" {
  name                   = "${local.prefix}-bounce-events"
  configuration_set_name = aws_ses_configuration_set.main.name
  enabled                = true

  matching_types = [
    "bounce",
    "complaint",
    "delivery",
  ]

  sns_destination {
    topic_arn = aws_sns_topic.ses_events.arn
  }
}

# ── Inbound: Receipt Rule Set ──────────────────────────────────────────────────

resource "aws_ses_receipt_rule_set" "main" {
  rule_set_name = "${local.prefix}-receipt-rules"
}

resource "aws_ses_active_receipt_rule_set" "main" {
  rule_set_name = aws_ses_receipt_rule_set.main.rule_set_name
}

# ── Inbound: Receipt Rule ──────────────────────────────────────────────────────
# SES receives → spam scan → writes RFC 5322 to S3 at inbound/raw/<messageId>
# S3 ObjectCreated notification then enqueues to email_inbound SQS (in s3.tf).
# The email_inbound Lambda parses MIME, stores to DB — no SPF/DKIM/DMARC code
# needed since SES already enforced all of that before accepting the message.

resource "aws_ses_receipt_rule" "store_to_s3" {
  name          = "${local.prefix}-store-to-s3"
  rule_set_name = aws_ses_receipt_rule_set.main.rule_set_name
  recipients    = [var.mail_domain]
  enabled       = true
  scan_enabled  = true
  tls_policy    = "Require"

  s3_action {
    bucket_name       = aws_s3_bucket.emails.id
    object_key_prefix = "inbound/raw/"
    position          = 1
  }

  depends_on = [aws_s3_bucket_policy.ses_write]
}

# ── S3 Bucket Policy: allow SES to write raw emails ───────────────────────────

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
  description = "Add these 3 CNAME records to DNS to enable DKIM for your mail domain"
  value       = aws_ses_domain_dkim.main.dkim_tokens
}

output "ses_verification_token" {
  description = "Add this TXT record to DNS to verify your mail domain in SES"
  value       = aws_ses_domain_identity.main.verification_token
}

output "ses_configuration_set" {
  description = "SES configuration set name — set as SES_CONFIGURATION_SET in email_outbound Lambda"
  value       = aws_ses_configuration_set.main.name
}
