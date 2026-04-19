# ── Embedder SQS Queue ────────────────────────────────────────────────────────
# The embedder subscribes to the events-fanout SNS topic via this queue.
# The SNS subscription itself lives in sns.tf.

resource "aws_sqs_queue" "embedder" {
  name                       = "${local.prefix}-embedder"
  message_retention_seconds  = 86400
  visibility_timeout_seconds = 60

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.embedder_dlq.arn
    maxReceiveCount     = 3
  })
}

resource "aws_sqs_queue" "embedder_dlq" {
  name                      = "${local.prefix}-embedder-dlq"
  message_retention_seconds = 1209600
}

resource "aws_sqs_queue_policy" "embedder" {
  queue_url = aws_sqs_queue.embedder.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSNSFanout"
        Effect = "Allow"
        Principal = {
          Service = "sns.amazonaws.com"
        }
        Action   = "sqs:SendMessage"
        Resource = aws_sqs_queue.embedder.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.events_fanout.arn
          }
        }
      }
    ]
  })
}

# ── SQS → Lambda Event Source Mappings ───────────────────────────────────────

resource "aws_lambda_event_source_mapping" "email_inbound" {
  event_source_arn        = aws_sqs_queue.email_inbound.arn
  function_name           = aws_lambda_function.email_inbound.arn
  batch_size              = 1
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "email_outbound" {
  event_source_arn        = aws_sqs_queue.email_outbound.arn
  function_name           = aws_lambda_function.email_outbound.arn
  batch_size              = 1
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "event_dispatcher_webhook" {
  event_source_arn        = aws_sqs_queue.webhook_delivery.arn
  function_name           = aws_lambda_function.event_dispatcher_webhook.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "event_dispatcher_ws" {
  event_source_arn        = aws_sqs_queue.ws_dispatch.arn
  function_name           = aws_lambda_function.event_dispatcher_ws.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

resource "aws_lambda_event_source_mapping" "embedder" {
  event_source_arn        = aws_sqs_queue.embedder.arn
  function_name           = aws_lambda_function.embedder.arn
  batch_size              = 1
  function_response_types = ["ReportBatchItemFailures"]
}

# ── EventBridge → Lambda Permissions ─────────────────────────────────────────
# Rules and targets are defined in eventbridge.tf

resource "aws_lambda_permission" "scheduler_bounce_eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.scheduler_bounce.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler_bounce.arn
}

resource "aws_lambda_permission" "scheduler_drafts_eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.scheduler_drafts.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler_drafts.arn
}

