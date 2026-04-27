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

resource "aws_cloudwatch_log_group" "lambda_ses_events" {
  name              = "/aws/lambda/${local.lambda_names.ses_events}"
  retention_in_days = local.log_retention_days
}

resource "aws_cloudwatch_log_group" "lambda_scheduler_drafts" {
  name              = "/aws/lambda/${local.lambda_names.scheduler_drafts}"
  retention_in_days = local.log_retention_days
}

# ── authorizer ────────────────────────────────────────────────────────────────
# Validates API keys — needs DB. Placed in VPC to reach private RDS Proxy.

resource "aws_lambda_function" "authorizer" {
  function_name    = local.lambda_names.authorizer
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/authorizer.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/authorizer.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_authorizer]
}

# ── orgs ──────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "orgs" {
  function_name    = local.lambda_names.orgs
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/orgs.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/orgs.zip")
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
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/auth.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/auth.zip")
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
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/inboxes.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/inboxes.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      MAIL_DOMAIN                = var.mail_domain
      EMAIL_OUTBOUND_QUEUE_URL   = aws_sqs_queue.email_outbound.url
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_inboxes]
}

# ── threads ───────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "threads" {
  function_name    = local.lambda_names.threads
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/threads.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/threads.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_threads]
}

# ── messages ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "messages" {
  function_name    = local.lambda_names.messages
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/messages.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/messages.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMAIL_OUTBOUND_QUEUE_URL   = aws_sqs_queue.email_outbound.url
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
      S3_BUCKET_ATTACHMENTS      = aws_s3_bucket.attachments.id
      S3_BUCKET_EMAILS           = aws_s3_bucket.emails.id
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_messages]
}

# ── drafts ────────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "drafts" {
  function_name    = local.lambda_names.drafts
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/drafts.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/drafts.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMAIL_OUTBOUND_QUEUE_URL   = aws_sqs_queue.email_outbound.url
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
      S3_BUCKET_ATTACHMENTS      = aws_s3_bucket.attachments.id
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_drafts]
}

# ── webhooks ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "webhooks" {
  function_name    = local.lambda_names.webhooks
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/webhooks.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/webhooks.zip")
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
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/search.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/search.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMBEDDER_URL     = var.embedder_url
      EMBEDDER_MODEL   = var.embedder_model
      EMBEDDER_API_KEY = var.embedder_api_key
      EMBEDDER_DIMS    = var.embedder_dims
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_search]
}

# ── ws_connect ────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "ws_connect" {
  function_name    = local.lambda_names.ws_connect
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/ws_connect.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/ws_connect.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ws_connect]
}

# ── ws_disconnect ─────────────────────────────────────────────────────────────

resource "aws_lambda_function" "ws_disconnect" {
  function_name    = local.lambda_names.ws_disconnect
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/ws_disconnect.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/ws_disconnect.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = local.db_env
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ws_disconnect]
}

# ── email_inbound ─────────────────────────────────────────────────────────────

resource "aws_lambda_function" "email_inbound" {
  function_name    = local.lambda_names.email_inbound
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/email_inbound.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/email_inbound.zip")
  timeout          = 300
  memory_size      = 512

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      S3_BUCKET_EMAILS           = aws_s3_bucket.emails.id
      S3_BUCKET_ATTACHMENTS      = aws_s3_bucket.attachments.id
      MAIL_DOMAIN                = var.mail_domain
      # Domain events: published directly to SQS — no SNS broker
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_email_inbound]
}

# ── email_outbound ────────────────────────────────────────────────────────────

resource "aws_lambda_function" "email_outbound" {
  function_name    = local.lambda_names.email_outbound
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/email_outbound.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/email_outbound.zip")
  timeout          = 300
  memory_size      = 512

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      S3_BUCKET_EMAILS           = aws_s3_bucket.emails.id
      S3_BUCKET_ATTACHMENTS      = aws_s3_bucket.attachments.id
      SES_CONFIGURATION_SET      = aws_ses_configuration_set.main.name
      # Domain events: published directly to SQS — no SNS broker
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_email_outbound]
}

# ── event_dispatcher_webhook ──────────────────────────────────────────────────

resource "aws_lambda_function" "event_dispatcher_webhook" {
  function_name    = local.lambda_names.event_dispatcher_webhook
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/event_dispatcher_webhook.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/event_dispatcher_webhook.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_event_dispatcher_webhook]
}

# ── event_dispatcher_ws ───────────────────────────────────────────────────────

resource "aws_lambda_function" "event_dispatcher_ws" {
  function_name    = local.lambda_names.event_dispatcher_ws
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/event_dispatcher_ws.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/event_dispatcher_ws.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      # Management API uses https://, not wss://
      APIGW_WEBSOCKET_ENDPOINT = replace(aws_apigatewayv2_stage.websocket.invoke_url, "wss://", "https://")
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_event_dispatcher_ws]
}

# ── embedder ──────────────────────────────────────────────────────────────────

resource "aws_lambda_function" "embedder" {
  function_name    = local.lambda_names.embedder
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/embedder.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/embedder.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMBEDDER_URL          = var.embedder_url
      EMBEDDER_MODEL        = var.embedder_model
      EMBEDDER_API_KEY      = var.embedder_api_key
      EMBEDDER_DIMS         = var.embedder_dims
      S3_BUCKET_EMAILS      = aws_s3_bucket.emails.id
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_embedder]
}

# ── ses_events ────────────────────────────────────────────────────────────────
# Processes SES bounce/complaint/delivery notifications from the ses_events SQS
# queue. Updates messages.status in Aurora and publishes message.bounced domain
# events directly to webhook-delivery and ws-dispatch SQS queues.
# Replaces the old scheduler_bounce polling job — SES pushes in real time.

resource "aws_lambda_function" "ses_events" {
  function_name    = local.lambda_names.ses_events
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/ses_events.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/ses_events.zip")
  timeout          = 60
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
      S3_BUCKET_EMAILS           = aws_s3_bucket.emails.id
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_ses_events]
}

# ── scheduler_drafts ──────────────────────────────────────────────────────────

resource "aws_lambda_function" "scheduler_drafts" {
  function_name    = local.lambda_names.scheduler_drafts
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/scheduler_drafts.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/scheduler_drafts.zip")
  timeout          = 300
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      EMAIL_OUTBOUND_QUEUE_URL   = aws_sqs_queue.email_outbound.url
      WEBHOOK_DELIVERY_QUEUE_URL = aws_sqs_queue.webhook_delivery.url
      WS_DISPATCH_QUEUE_URL      = aws_sqs_queue.ws_dispatch.url
      EMBEDDER_QUEUE_URL         = aws_sqs_queue.embedder.url
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_scheduler_drafts]
}

# ── domains ───────────────────────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "lambda_domains" {
  name              = "/aws/lambda/${local.lambda_names.domains}"
  retention_in_days = local.log_retention_days
}

resource "aws_lambda_function" "domains" {
  function_name    = local.lambda_names.domains
  role             = aws_iam_role.lambda.arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  filename         = "${path.module}/../dist/lambdas/domains.zip"
  source_code_hash = filebase64sha256("${path.module}/../dist/lambdas/domains.zip")
  timeout          = 30
  memory_size      = 256

  vpc_config {
    subnet_ids         = local.lambda_subnet_ids
    security_group_ids = [aws_security_group.lambda.id]
  }

  environment {
    variables = merge(local.db_env, {
      SES_RULE_SET_NAME = aws_ses_receipt_rule_set.main.rule_set_name
      S3_BUCKET_EMAILS  = aws_s3_bucket.emails.id
    })
  }

  depends_on = [aws_cloudwatch_log_group.lambda_domains]
}
