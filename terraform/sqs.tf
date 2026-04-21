# ── SQS: Email Inbound ────────────────────────────────────────────────────────
# Triggered by S3 ObjectCreated notifications when SES stores inbound email.

resource "aws_sqs_queue" "email_inbound" {
  name                       = local.sqs_names.email_inbound
  visibility_timeout_seconds = 300
  message_retention_seconds  = 604800 # 7 days
}

# S3 requires a queue policy to send ObjectCreated notifications
resource "aws_sqs_queue_policy" "email_inbound" {
  queue_url = aws_sqs_queue.email_inbound.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowS3Notifications"
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
# Published to by inbox/draft Lambdas. Consumed by email_outbound Lambda
# which calls ses:SendRawEmail. FIFO preserves per-message ordering.

resource "aws_sqs_queue" "email_outbound" {
  name                        = local.sqs_names.email_outbound
  fifo_queue                  = true
  content_based_deduplication = true
  visibility_timeout_seconds  = 300
  message_retention_seconds   = 86400 # 24 hours
}

# ── SQS: Webhook Delivery DLQ ─────────────────────────────────────────────────
# Defined before webhook_delivery so its ARN is available for the redrive policy.

resource "aws_sqs_queue" "webhook_dlq" {
  name                      = local.sqs_names.webhook_dlq
  message_retention_seconds = 1209600 # 14 days for manual inspection
}

# ── SQS: Webhook Delivery ─────────────────────────────────────────────────────
# Domain event Lambdas publish directly here (no SNS broker).
# event_dispatcher_webhook Lambda reads, looks up subscriptions, delivers HTTP.

resource "aws_sqs_queue" "webhook_delivery" {
  name                       = local.sqs_names.webhook_delivery
  visibility_timeout_seconds = 120
  message_retention_seconds  = 259200 # 3 days

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.webhook_dlq.arn
    maxReceiveCount     = var.webhook_max_retries
  })
}

# ── SQS: WebSocket Dispatch ───────────────────────────────────────────────────
# Domain event Lambdas publish directly here.
# event_dispatcher_ws Lambda reads, queries websocket_connections in DB,
# calls ApiGatewayManagementApi.PostToConnection per live connection.

resource "aws_sqs_queue" "ws_dispatch" {
  name                       = local.sqs_names.ws_dispatch
  visibility_timeout_seconds = 60
  message_retention_seconds  = 300 # 5 minutes — stale WebSocket events are useless
}

# ── SQS: Embedder ─────────────────────────────────────────────────────────────
# Domain event Lambdas publish message events here.
# embedder Lambda reads, generates vector embeddings, stores in Aurora.

resource "aws_sqs_queue" "embedder" {
  name                       = local.sqs_names.embedder
  visibility_timeout_seconds = 120
  message_retention_seconds  = 86400 # 24 hours

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.embedder_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "embedder_dlq" {
  name                      = "${local.prefix}-embedder-dlq"
  message_retention_seconds = 86400
}

# ── SQS: SES Events ───────────────────────────────────────────────────────────
# SES Configuration Set publishes bounce/complaint/delivery events to the
# ses_events SNS topic, which fans out to this queue.
# ses_events Lambda reads and updates message status + publishes domain events.

resource "aws_sqs_queue" "ses_events" {
  name                       = local.sqs_names.ses_events
  visibility_timeout_seconds = 60
  message_retention_seconds  = 86400 # 24 hours
}

# SNS requires a queue policy to deliver to SQS
resource "aws_sqs_queue_policy" "ses_events" {
  queue_url = aws_sqs_queue.ses_events.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSDelivery"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.ses_events.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.ses_events.arn
          }
        }
      }
    ]
  })
}
