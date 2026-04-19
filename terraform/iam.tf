# ── Lambda Execution Role ─────────────────────────────────────────────────────

resource "aws_iam_role" "lambda" {
  name = "${local.prefix}-lambda-exec"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })
}

# ── Managed Policy Attachments ────────────────────────────────────────────────

resource "aws_iam_role_policy_attachment" "lambda_vpc" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"
}

# ── Inline Application Policy ─────────────────────────────────────────────────

resource "aws_iam_role_policy" "lambda_app" {
  name = "${local.prefix}-lambda-app"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "SQSAccess"
        Effect = "Allow"
        Action = [
          "sqs:SendMessage",
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes",
          "sqs:ChangeMessageVisibility",
        ]
        Resource = [
          aws_sqs_queue.email_inbound.arn,
          aws_sqs_queue.email_outbound.arn,
          aws_sqs_queue.webhook_delivery.arn,
          aws_sqs_queue.webhook_dlq.arn,
          aws_sqs_queue.ws_dispatch.arn,
          aws_sqs_queue.embedder.arn,
        ]
      },
      {
        Sid    = "SNSPublish"
        Effect = "Allow"
        Action = [
          "sns:Publish",
        ]
        Resource = aws_sns_topic.events_fanout.arn
      },
      {
        Sid    = "S3EmailsAttachments"
        Effect = "Allow"
        Action = [
          "s3:PutObject",
          "s3:GetObject",
          "s3:DeleteObject",
          "s3:HeadObject",
        ]
        Resource = [
          "${aws_s3_bucket.emails.arn}/*",
          "${aws_s3_bucket.attachments.arn}/*",
        ]
      },
      {
        Sid    = "S3ArtifactsRead"
        Effect = "Allow"
        Action = [
          "s3:GetObject",
        ]
        Resource = "${aws_s3_bucket.artifacts.arn}/*"
      },
      {
        Sid    = "SecretsManagerRead"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
        ]
        Resource = aws_secretsmanager_secret.db_password.arn
      },
      {
        Sid    = "ApiGatewayManageConnections"
        Effect = "Allow"
        Action = [
          "execute-api:ManageConnections",
        ]
        Resource = "arn:aws:execute-api:${var.aws_region}:*:*/@connections/*"
      },
      {
        Sid    = "CloudWatchLogs"
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents",
        ]
        Resource = "arn:aws:logs:*:*:*"
      },
    ]
  })
}
