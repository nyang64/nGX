# EventBridge scheduled rules.
#
# Bounce detection is removed — SES Configuration Set publishes bounce/complaint
# events in real time via SNS ses_events → SQS ses_events → ses_events Lambda.
# No polling scheduler needed for bounces.
#
# Only draft expiry remains as a periodic job.

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
