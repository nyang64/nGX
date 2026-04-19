# ── Shared locals ─────────────────────────────────────────────────────────────

locals {
  # Private subnet IDs used by all VPC-attached Lambdas
  lambda_subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]

  # Common DB environment variables injected into every DB-connected Lambda
  db_env = {
    DATABASE_URL     = "postgres://${var.db_username}:${random_password.db_password.result}@${aws_db_proxy.main.endpoint}:5432/${var.db_name}?sslmode=require"
    DB_MAX_CONNS     = "5"
    DB_MIN_CONNS     = "1"
    ENVIRONMENT      = var.environment
    AWS_REGION_NAME  = var.aws_region
  }
}

# ── CloudWatch Log Groups ─────────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "lambda_authorizer" {
  name              = "/aws/lambda/${local.lambda_names.authorizer}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_orgs" {
  name              = "/aws/lambda/${local.lambda_names.orgs}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_auth" {
  name              = "/aws/lambda/${local.lambda_names.auth}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_inboxes" {
  name              = "/aws/lambda/${local.lambda_names.inboxes}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_threads" {
  name              = "/aws/lambda/${local.lambda_names.threads}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_messages" {
  name              = "/aws/lambda/${local.lambda_names.messages}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_drafts" {
  name              = "/aws/lambda/${local.lambda_names.drafts}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_webhooks" {
  name              = "/aws/lambda/${local.lambda_names.webhooks}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_search" {
  name              = "/aws/lambda/${local.lambda_names.search}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_ws_connect" {
  name              = "/aws/lambda/${local.lambda_names.ws_connect}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_ws_disconnect" {
  name              = "/aws/lambda/${local.lambda_names.ws_disconnect}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_email_inbound" {
  name              = "/aws/lambda/${local.lambda_names.email_inbound}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_email_outbound" {
  name              = "/aws/lambda/${local.lambda_names.email_outbound}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_event_dispatcher_webhook" {
  name              = "/aws/lambda/${local.lambda_names.event_dispatcher_webhook}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_event_dispatcher_ws" {
  name              = "/aws/lambda/${local.lambda_names.event_dispatcher_ws}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_embedder" {
  name              = "/aws/lambda/${local.lambda_names.embedder}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_scheduler_bounce" {
  name              = "/aws/lambda/${local.lambda_names.scheduler_bounce}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_scheduler_drafts" {
  name              = "/aws/lambda/${local.lambda_names.scheduler_drafts}"
  retention_in_days = local.log_retention_days
}

# ── authorizer ────────────────────────────────────────────────────────────────
# Validates API keys — needs DB but skips VPC for lower cold-start latency.
# Run without VPC; uses public RDS Proxy endpoint via internet.
# NOTE: If your RDS Proxy is private-only, move this into VPC config.

resource "aws_lambda_function" "authorizer" {
  function_name    = local.lambda_names.authorizer
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_authorizer]
}

# ── orgs ──────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "orgs" {
  function_name    = local.lambda_names.orgs
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_orgs]
}

# ── auth ──────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "auth" {
  function_name    = local.lambda_names.auth
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_auth]
}

# ── inboxes ───────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "inboxes" {
  function_name    = local.lambda_names.inboxes
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_inboxes]
}

# ── threads ───────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "threads" {
  function_name    = local.lambda_names.threads
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_threads]
}

# ── messages ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "messages" {
  function_name    = local.lambda_names.messages
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_messages]
}

# ── drafts ────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "drafts" {
  function_name    = local.lambda_names.drafts
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_drafts]
}

# ── webhooks ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "webhooks" {
  function_name    = local.lambda_names.webhooks
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      WEBHOOK_MAX_RETRIES       = tostring(var.webhook_max_retries)
      WEBHOOK_ENCRYPTION_KEY    = var.webhook_encryption_key
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_webhooks]
}

# ── search ────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "search" {
  function_name    = local.lambda_names.search
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMBEDDER_URL   = var.embedder_url
      EMBEDDER_MODEL = var.embedder_model
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_search]
}

# ── ws_connect ────────────────────────────────────────────────────────────────
# Inserts connectionId into websocket_connections table — needs DB, skips VPC
# for same reason as authorizer (fast WebSocket handshake path).
# NOTE: Move into VPC if RDS Proxy is private-only.

resource "aws_lambda_function" "ws_connect" {
  function_name    = local.lambda_names.ws_connect
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ws_connect]
}

# ── ws_disconnect ─────────────────────────────────────────────────────────────

resource "aws_lambda_function" "ws_disconnect" {
  function_name    = local.lambda_names.ws_disconnect
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ws_disconnect]
}

# ── email_inbound ─────────────────────────────────────────────────────────────

resource "aws_lambda_function" "email_inbound" {
  function_name    = local.lambda_names.email_inbound
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 300
  memory_size      = 512

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      S3_BUCKET_EMAILS       = aws_s3_bucket.emails.id
      S3_BUCKET_ATTACHMENTS  = aws_s3_bucket.attachments.id
      EVENTS_FANOUT_TOPIC_ARN = aws_sns_topic.events_fanout.arn
      MAIL_DOMAIN            = var.mail_domain
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_email_inbound]
}

# ── email_outbound ────────────────────────────────────────────────────────────

resource "aws_lambda_function" "email_outbound" {
  function_name    = local.lambda_names.email_outbound
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 300
  memory_size      = 512

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      S3_BUCKET_EMAILS        = aws_s3_bucket.emails.id
      S3_BUCKET_ATTACHMENTS   = aws_s3_bucket.attachments.id
      SMTP_RELAY_HOST         = var.smtp_relay_host
      DKIM_SELECTOR           = var.dkim_selector
      EVENTS_FANOUT_TOPIC_ARN = aws_sns_topic.events_fanout.arn
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_email_outbound]
}

# ── event_dispatcher_webhook ──────────────────────────────────────────────────

resource "aws_lambda_function" "event_dispatcher_webhook" {
  function_name    = local.lambda_names.event_dispatcher_webhook
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      EVENTS_FANOUT_TOPIC_ARN    = aws_sns_topic.events_fanout.arn
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_event_dispatcher_webhook]
}

# ── event_dispatcher_ws ───────────────────────────────────────────────────────

resource "aws_lambda_function" "event_dispatcher_ws" {
  function_name    = local.lambda_names.event_dispatcher_ws
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      # Cannot reference aws_apigatewayv2_api.websocket.api_endpoint here —
      # that would create a dependency cycle through the Lambda permission.
      # Set this value post-deploy via: aws lambda update-function-configuration
      APIGW_WEBSOCKET_ENDPOINT = "CONFIGURE_POST_DEPLOY"
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_event_dispatcher_ws]
}

# ── embedder ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "embedder" {
  function_name    = local.lambda_names.embedder
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMBEDDER_URL            = var.embedder_url
      EMBEDDER_MODEL          = var.embedder_model
      S3_BUCKET_EMAILS        = aws_s3_bucket.emails.id
      EVENTS_FANOUT_TOPIC_ARN = aws_sns_topic.events_fanout.arn
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_embedder]
}

# ── scheduler_bounce ──────────────────────────────────────────────────────────

resource "aws_lambda_function" "scheduler_bounce" {
  function_name    = local.lambda_names.scheduler_bounce
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 300
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_scheduler_bounce]
}

# ── scheduler_drafts ──────────────────────────────────────────────────────────

resource "aws_lambda_function" "scheduler_drafts" {
  function_name    = local.lambda_names.scheduler_drafts
  role             = aws_iam_role.lambda.arn
  runtime          = "python3.12"
  handler          = "handler.handler"
  filename         = data.archive_file.lambda_stub.output_path
  source_code_hash = data.archive_file.lambda_stub.output_base64sha256
  timeout          = 300
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_scheduler_drafts]
}
