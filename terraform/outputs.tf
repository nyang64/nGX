# ── API Endpoints ──────────────────────────────────────────────────────────────

output "rest_api_endpoint" {
  description = "Base URL for the REST API"
  value       = "https://${aws_api_gateway_rest_api.main.id}.execute-api.${var.aws_region}.amazonaws.com/${var.environment}"
}

output "websocket_endpoint" {
  description = "WebSocket API endpoint (wss://). Use this for APIGW_WEBSOCKET_ENDPOINT in event_dispatcher_ws Lambda."
  value       = aws_apigatewayv2_stage.websocket.invoke_url
}

# ── Database ───────────────────────────────────────────────────────────────────

output "rds_proxy_endpoint" {
  description = "RDS Proxy endpoint — used in DATABASE_URL for all Lambda functions"
  value       = aws_db_proxy.main.endpoint
}

output "aurora_cluster_endpoint" {
  description = "Aurora cluster writer endpoint (use RDS Proxy in application code)"
  value       = aws_rds_cluster.main.endpoint
  sensitive   = true
}

output "db_secret_arn" {
  description = "Secrets Manager ARN for the database credentials"
  value       = aws_secretsmanager_secret.db_password.arn
}

# ── Storage ────────────────────────────────────────────────────────────────────

output "s3_bucket_emails" {
  description = "S3 bucket name for email storage"
  value       = aws_s3_bucket.emails.id
}

output "s3_bucket_attachments" {
  description = "S3 bucket name for attachment storage"
  value       = aws_s3_bucket.attachments.id
}

output "s3_bucket_artifacts" {
  description = "S3 bucket name for Lambda deployment artifacts"
  value       = aws_s3_bucket.artifacts.id
}

# ── Queues ─────────────────────────────────────────────────────────────────────

output "sqs_email_inbound_url" {
  description = "SQS queue URL for inbound email jobs"
  value       = aws_sqs_queue.email_inbound.url
}

output "sqs_email_outbound_url" {
  description = "SQS FIFO queue URL for outbound email jobs"
  value       = aws_sqs_queue.email_outbound.url
}

output "sqs_webhook_delivery_url" {
  description = "SQS queue URL for webhook delivery tasks"
  value       = aws_sqs_queue.webhook_delivery.url
}

output "ses_events_queue_url" {
  description = "SQS queue URL for SES bounce/complaint events"
  value       = aws_sqs_queue.ses_events.url
}

# ── Networking ─────────────────────────────────────────────────────────────────

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}

output "private_subnet_ids" {
  description = "Private subnet IDs (Lambda + Aurora)"
  value       = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

# ── Post-deploy action ─────────────────────────────────────────────────────────

output "post_deploy_instructions" {
  description = "Manual steps required after first terraform apply"
  value       = <<-EOT
    Post-deploy checklist:
    1. Update event_dispatcher_ws Lambda APIGW_WEBSOCKET_ENDPOINT env var:
       aws lambda update-function-configuration \
         --function-name ${aws_lambda_function.event_dispatcher_ws.function_name} \
         --environment "Variables={APIGW_WEBSOCKET_ENDPOINT=${aws_apigatewayv2_stage.websocket.invoke_url}}"

    2. Run database migrations against the Aurora cluster via RDS Proxy:
       DATABASE_URL=postgres://${var.db_username}:<password>@${aws_db_proxy.main.endpoint}:5432/${var.db_name}?sslmode=require \
       make migrate-up

    3. Verify SES domain identity for: ${var.mail_domain}
       Add the TXT/MX DNS records shown in the AWS SES console.

    4. Deploy Lambda function code:
       make build-lambdas && make deploy-lambdas
  EOT
}
