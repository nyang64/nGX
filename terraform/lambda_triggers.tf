# ── SQS → Lambda Event Source Mappings ───────────────────────────────────────
# Embedder queue is now defined in sqs.tf alongside the other queues.
# All queues use ReportBatchItemFailures so partial batch failures are
# re-tried without reprocessing successfully handled messages.

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

# SES bounce/complaint events: SNS ses_events → SQS ses_events → ses_events Lambda
resource "aws_lambda_event_source_mapping" "ses_events" {
  event_source_arn        = aws_sqs_queue.ses_events.arn
  function_name           = aws_lambda_function.ses_events.arn
  batch_size              = 10
  function_response_types = ["ReportBatchItemFailures"]
}

# ── EventBridge → Lambda Permission ──────────────────────────────────────────
# scheduler_bounce is removed — SES handles bounce detection via ses_events Lambda.
# eventbridge.tf defines the rule and target; permission granted here.

resource "aws_lambda_permission" "scheduler_drafts_eventbridge" {
  statement_id  = "AllowEventBridgeInvokeSchedulerDrafts"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.scheduler_drafts.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.scheduler_drafts.arn
}
