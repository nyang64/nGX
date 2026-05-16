# ── License Infrastructure ────────────────────────────────────────────────────

# SSM parameter for license token — value written by activation, not Terraform
resource "aws_ssm_parameter" "license_token" {
  name  = "/ngx/license-token"
  type  = "SecureString"
  value = "placeholder" # overwritten by activation process

  lifecycle {
    ignore_changes = [value]
  }
}

resource "aws_ssm_parameter" "bootstrap_org_id" {
  name  = "/ngx/bootstrap-org-id"
  type  = "String"
  value = "placeholder"

  lifecycle {
    ignore_changes = [value]
  }
}

# CloudWatch alarm: fire if license_refresh Lambda has any errors in 25-hour window
resource "aws_cloudwatch_metric_alarm" "license_refresh_errors" {
  alarm_name          = "${local.prefix}-license-refresh-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 90000 # 25 hours in seconds
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "License refresh Lambda failed — token will expire in <7 days if not fixed"
  treat_missing_data  = "notBreaching"

  dimensions = {
    FunctionName = aws_lambda_function.license_refresh.function_name
  }

  alarm_actions = [aws_sns_topic.license_alerts.arn]
  ok_actions    = [aws_sns_topic.license_alerts.arn]
}

resource "aws_sns_topic" "license_alerts" {
  name = "${local.prefix}-license-alerts"
}

resource "aws_sns_topic_subscription" "license_alerts_email" {
  topic_arn = aws_sns_topic.license_alerts.arn
  protocol  = "email"
  endpoint  = "nyang63@gmail.com"
}
