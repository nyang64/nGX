# SNS is used only for SES event delivery (bounces, complaints, deliveries).
# SES requires SNS as an event destination — it cannot publish directly to SQS.
# Domain events (message.received, draft.approved, etc.) are published directly
# from Lambda to SQS queues — no general-purpose fan-out broker needed.

resource "aws_sns_topic" "ses_events" {
  name = "${local.prefix}-ses-events"
}

# Allow SES to publish to this topic
resource "aws_sns_topic_policy" "ses_events" {
  arn = aws_sns_topic.ses_events.arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "AllowSESPublish"
        Effect = "Allow"
        Principal = {
          Service = "ses.amazonaws.com"
        }
        Action   = "sns:Publish"
        Resource = aws_sns_topic.ses_events.arn
        Condition = {
          StringEquals = {
            "aws:SourceAccount" = data.aws_caller_identity.current.account_id
          }
        }
      }
    ]
  })
}

# SNS → SQS: route SES events to the ses-events queue for Lambda processing
resource "aws_sns_topic_subscription" "ses_events_sqs" {
  topic_arn            = aws_sns_topic.ses_events.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.ses_events.arn
  raw_message_delivery = true
}
