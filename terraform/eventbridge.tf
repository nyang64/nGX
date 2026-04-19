# ── EventBridge Scheduled Jobs ────────────────────────────────────────────────
#
# These rules replace the scheduler service by invoking Lambda functions
# directly on a schedule via CloudWatch Events (EventBridge).

# ── Bounce Check: every hour ──────────────────────────────────────────────────

resource "aws_cloudwatch_event_rule" "scheduler_bounce" {
  name                = "${local.prefix}-scheduler-bounce"
  description         = "Hourly bounce check"
  schedule_expression = "rate(1 hour)"
}

resource "aws_cloudwatch_event_target" "scheduler_bounce" {
  rule = aws_cloudwatch_event_rule.scheduler_bounce.name
  arn  = aws_lambda_function.scheduler_bounce.arn
}

resource "aws_lambda_permission" "scheduler_bounce_events" {
  statement_id  = "AllowEventBridgeInvokeSchedulerBounce"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.scheduler_bounce.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler_bounce.arn
}

# ── Draft Expiry Check: every 5 minutes ──────────────────────────────────────

resource "aws_cloudwatch_event_rule" "scheduler_drafts" {
  name                = "${local.prefix}-scheduler-drafts"
  description         = "Draft expiry check every 5 minutes"
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
