# ── SQS: Email Inbound ────────────────────────────────────────────────────────

resource "aws_sqs_queue" "email_inbound" {
  name                       = local.sqs_names.email_inbound
  visibility_timeout_seconds = 300
  message_retention_seconds  = 604800 # 7 days
}

resource "aws_sqs_queue_policy" "email_inbound" {
  queue_url = aws_sqs_queue.email_inbound.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSSendMessage"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.email_inbound.arn
      },
      {
        Sid    = "AllowS3SendMessage"
        Effect = "Allow"
        Principal = {
          Service = "s3.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.email_inbound.arn
        Condition = {
          ArnLike = {
            "aws:SourceArn" = "arn:aws:s3:::${local.s3_names.emails}"
          }
        }
      }
    ]
  })
}

# ── SQS: Email Outbound (FIFO) ────────────────────────────────────────────────

resource "aws_sqs_queue" "email_outbound" {
  name                        = local.sqs_names.email_outbound
  fifo_queue                  = true
  content_based_deduplication = true
  visibility_timeout_seconds  = 300
  message_retention_seconds   = 86400 # 24 hours
}

resource "aws_sqs_queue_policy" "email_outbound" {
  queue_url = aws_sqs_queue.email_outbound.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSSendMessage"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.email_outbound.arn
      }
    ]
  })
}

# ── SQS: Webhook DLQ ──────────────────────────────────────────────────────────
# Must be defined before webhook_delivery so its ARN is available for the
# redrive policy.

resource "aws_sqs_queue" "webhook_dlq" {
  name                      = local.sqs_names.webhook_dlq
  message_retention_seconds = 1209600 # 14 days
}

resource "aws_sqs_queue_policy" "webhook_dlq" {
  queue_url = aws_sqs_queue.webhook_dlq.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSSendMessage"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.webhook_dlq.arn
      }
    ]
  })
}

# ── SQS: Webhook Delivery ─────────────────────────────────────────────────────

resource "aws_sqs_queue" "webhook_delivery" {
  name                       = local.sqs_names.webhook_delivery
  visibility_timeout_seconds = 120
  message_retention_seconds  = 259200 # 3 days

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.webhook_dlq.arn
    maxReceiveCount     = var.webhook_max_retries
  })
}

resource "aws_sqs_queue_policy" "webhook_delivery" {
  queue_url = aws_sqs_queue.webhook_delivery.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSSendMessage"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.webhook_delivery.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.events_fanout.arn
          }
        }
      }
    ]
  })
}

# ── SQS: WebSocket Dispatch ───────────────────────────────────────────────────

resource "aws_sqs_queue" "ws_dispatch" {
  name                       = local.sqs_names.ws_dispatch
  visibility_timeout_seconds = 60
  message_retention_seconds  = 300 # 5 minutes — stale WS events are useless
}

resource "aws_sqs_queue_policy" "ws_dispatch" {
  queue_url = aws_sqs_queue.ws_dispatch.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSSendMessage"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.ws_dispatch.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.events_fanout.arn
          }
        }
      }
    ]
  })
}
