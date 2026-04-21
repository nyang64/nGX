# EventBridge rules.
#
# 1. ses_events  — matches native SES events (Bounce/Complaint/Delivery) on the
#                  default event bus and routes them to the ses_events SQS queue.
#                  No SNS topic needed; SES publishes to EventBridge automatically.
#
# 2. scheduler_drafts — periodic job (rate 5m) to expire stale drafts.

# ── SES Events ────────────────────────────────────────────────────────────────

resource "aws_cloudwatch_event_rule" "ses_events" {
  name        = "${local.prefix}-ses-events"
  description = "Route SES bounce/complaint/delivery events to ses_events SQS queue"

  event_pattern = jsonencode({
    source      = ["aws.ses"]
    detail-type = ["SES Bounce", "SES Complaint", "SES Message Delivery"]
  })
}

resource "aws_cloudwatch_event_target" "ses_events" {
  rule = aws_cloudwatch_event_rule.ses_events.name
  arn  = aws_sqs_queue.ses_events.arn
}

# ── Draft Expiry ───────────────────────────────────────────────────────────────

resource "aws_cloudwatch_event_rule" "scheduler_drafts" {
  name                = "${local.prefix}-scheduler-drafts"
  description         = "Expire pending drafts past their retention window"
  schedule_expression = "rate(5 minutes)"
}

resource "aws_cloudwatch_event_target" "scheduler_drafts" {
  rule = aws_cloudwatch_event_rule.scheduler_drafts.name
  arn  = aws_lambda_function.scheduler_drafts.arn
}

resource "aws_lambda_permission" "scheduler_drafts_events" {
  statement_id  = "AllowEventBridgeInvokeSchedulerDrafts"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.scheduler_drafts.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler_drafts.arn
}
