# ── SNS: Events Fanout Topic ──────────────────────────────────────────────────

resource "aws_sns_topic" "events_fanout" {
  name = "${local.prefix}-events-fanout"
}

# ── SNS → SQS Subscriptions ───────────────────────────────────────────────────

resource "aws_sns_topic_subscription" "webhook" {
  topic_arn            = aws_sns_topic.events_fanout.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.webhook_delivery.arn
  raw_message_delivery = true
}

resource "aws_sns_topic_subscription" "ws_dispatch" {
  topic_arn            = aws_sns_topic.events_fanout.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.ws_dispatch.arn
  raw_message_delivery = true
}

resource "aws_sns_topic_subscription" "embedder" {
  topic_arn            = aws_sns_topic.events_fanout.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.embedder.arn
  raw_message_delivery = true
}
