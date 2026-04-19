locals {
  # Resource name prefix used consistently across all resources
  prefix = "${var.app_name}-${var.environment}"

  # Common tags merged onto every resource via provider default_tags
  common_tags = merge({
    Application = var.app_name
    Environment = var.environment
    ManagedBy   = "terraform"
  }, var.tags)

  # Two AZs for HA (Aurora requires ≥2, RDS Proxy benefits from ≥2)
  azs = ["${var.aws_region}a", "${var.aws_region}b"]

  # ── Lambda function names ────────────────────────────────────────────────────
  lambda_names = {
    authorizer               = "${local.prefix}-authorizer"
    orgs                     = "${local.prefix}-orgs"
    auth                     = "${local.prefix}-auth"
    inboxes                  = "${local.prefix}-inboxes"
    threads                  = "${local.prefix}-threads"
    messages                 = "${local.prefix}-messages"
    drafts                   = "${local.prefix}-drafts"
    webhooks                 = "${local.prefix}-webhooks"
    search                   = "${local.prefix}-search"
    ws_connect               = "${local.prefix}-ws-connect"
    ws_disconnect            = "${local.prefix}-ws-disconnect"
    email_inbound            = "${local.prefix}-email-inbound"
    email_outbound           = "${local.prefix}-email-outbound"
    event_dispatcher_webhook = "${local.prefix}-event-dispatcher-webhook"
    event_dispatcher_ws      = "${local.prefix}-event-dispatcher-ws"
    embedder                 = "${local.prefix}-embedder"
    scheduler_bounce         = "${local.prefix}-scheduler-bounce"
    scheduler_drafts         = "${local.prefix}-scheduler-drafts"
  }

  # ── SQS queue names ─────────────────────────────────────────────────────────
  # email-outbound is FIFO (.fifo suffix is required by AWS)
  sqs_names = {
    email_inbound    = "${local.prefix}-email-inbound-raw"
    email_outbound   = "${local.prefix}-email-outbound-queue.fifo"
    webhook_delivery = "${local.prefix}-webhook-delivery"
    webhook_dlq      = "${local.prefix}-webhook-delivery-dlq"
    ws_dispatch      = "${local.prefix}-ws-dispatch"
  }

  # ── S3 bucket names ──────────────────────────────────────────────────────────
  s3_names = {
    emails      = "${local.prefix}-emails"
    attachments = "${local.prefix}-attachments"
    artifacts   = "${local.prefix}-lambda-artifacts"
  }

  # ── CloudWatch log group names ───────────────────────────────────────────────
  log_retention_days = 30
}
