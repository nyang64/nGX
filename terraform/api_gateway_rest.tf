# ── REST API ──────────────────────────────────────────────────────────────────

resource "aws_api_gateway_rest_api" "main" {
  name        = "${local.prefix}-api"
  description = "nGX REST API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

# ── Custom Authorizer ─────────────────────────────────────────────────────────

resource "aws_api_gateway_authorizer" "api_key" {
  name                             = "${local.prefix}-api-key-authorizer"
  rest_api_id                      = aws_api_gateway_rest_api.main.id
  type                             = "TOKEN"
  authorizer_uri                   = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.authorizer.arn}/invocations"
  identity_source                  = "method.request.header.Authorization"
  authorizer_result_ttl_in_seconds = 300
}

# ── Gateway Responses ─────────────────────────────────────────────────────────

resource "aws_api_gateway_gateway_response" "unauthorized" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  response_type = "UNAUTHORIZED"
  status_code   = "401"

  response_templates = {
    "application/json" = jsonencode({
      error   = "Unauthorized"
      message = "Missing or invalid Authorization token"
    })
  }

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}

resource "aws_api_gateway_gateway_response" "access_denied" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  response_type = "ACCESS_DENIED"
  status_code   = "403"

  response_templates = {
    "application/json" = jsonencode({
      error   = "Forbidden"
      message = "You do not have permission to access this resource"
    })
  }

  response_parameters = {
    "gatewayresponse.header.Access-Control-Allow-Origin"  = "'*'"
    "gatewayresponse.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization'"
  }
}

# ── Deployment ────────────────────────────────────────────────────────────────

resource "aws_api_gateway_deployment" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id

  depends_on = [
    # /v1/org
    aws_api_gateway_integration.get_v1_org,
    aws_api_gateway_integration.patch_v1_org,
    aws_api_gateway_integration.options_v1_org,
    # /v1/pods
    aws_api_gateway_integration.get_v1_pods,
    aws_api_gateway_integration.post_v1_pods,
    aws_api_gateway_integration.options_v1_pods,
    # /v1/pods/{podId}
    aws_api_gateway_integration.get_v1_pods_pod_id,
    aws_api_gateway_integration.patch_v1_pods_pod_id,
    aws_api_gateway_integration.delete_v1_pods_pod_id,
    aws_api_gateway_integration.options_v1_pods_pod_id,
    # /v1/keys
    aws_api_gateway_integration.get_v1_keys,
    aws_api_gateway_integration.post_v1_keys,
    aws_api_gateway_integration.options_v1_keys,
    # /v1/keys/{keyId}
    aws_api_gateway_integration.get_v1_keys_key_id,
    aws_api_gateway_integration.delete_v1_keys_key_id,
    aws_api_gateway_integration.options_v1_keys_key_id,
    # /v1/inboxes
    aws_api_gateway_integration.get_v1_inboxes,
    aws_api_gateway_integration.post_v1_inboxes,
    aws_api_gateway_integration.options_v1_inboxes,
    # /v1/inboxes/{inboxId}
    aws_api_gateway_integration.get_v1_inboxes_inbox_id,
    aws_api_gateway_integration.patch_v1_inboxes_inbox_id,
    aws_api_gateway_integration.delete_v1_inboxes_inbox_id,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id,
    # /v1/inboxes/{inboxId}/threads
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads,
    # /v1/inboxes/{inboxId}/threads/{threadId}
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id,
    aws_api_gateway_integration.patch_v1_inboxes_inbox_id_threads_thread_id,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id,
    # /v1/inboxes/{inboxId}/threads/{threadId}/labels/{labelId}
    aws_api_gateway_integration.put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id,
    aws_api_gateway_integration.delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id,
    # /v1/inboxes/{inboxId}/threads/{threadId}/messages
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id_messages,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_messages,
    # /v1/inboxes/{inboxId}/threads/{threadId}/messages/{messageId}
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id,
    # /v1/inboxes/{inboxId}/messages/send
    aws_api_gateway_integration.post_v1_inboxes_inbox_id_messages_send,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_messages_send,
    # /v1/inboxes/{inboxId}/drafts
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_drafts,
    aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts,
    # /v1/inboxes/{inboxId}/drafts/{draftId}
    aws_api_gateway_integration.get_v1_inboxes_inbox_id_drafts_draft_id,
    aws_api_gateway_integration.patch_v1_inboxes_inbox_id_drafts_draft_id,
    aws_api_gateway_integration.delete_v1_inboxes_inbox_id_drafts_draft_id,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id,
    # /v1/inboxes/{inboxId}/drafts/{draftId}/approve
    aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts_draft_id_approve,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id_approve,
    # /v1/inboxes/{inboxId}/drafts/{draftId}/reject
    aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts_draft_id_reject,
    aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id_reject,
    # /v1/labels
    aws_api_gateway_integration.get_v1_labels,
    aws_api_gateway_integration.post_v1_labels,
    aws_api_gateway_integration.options_v1_labels,
    # /v1/labels/{labelId}
    aws_api_gateway_integration.patch_v1_labels_label_id,
    aws_api_gateway_integration.delete_v1_labels_label_id,
    aws_api_gateway_integration.options_v1_labels_label_id,
    # /v1/webhooks
    aws_api_gateway_integration.get_v1_webhooks,
    aws_api_gateway_integration.post_v1_webhooks,
    aws_api_gateway_integration.options_v1_webhooks,
    # /v1/webhooks/{webhookId}
    aws_api_gateway_integration.get_v1_webhooks_webhook_id,
    aws_api_gateway_integration.patch_v1_webhooks_webhook_id,
    aws_api_gateway_integration.delete_v1_webhooks_webhook_id,
    aws_api_gateway_integration.options_v1_webhooks_webhook_id,
    # /v1/webhooks/{webhookId}/deliveries
    aws_api_gateway_integration.get_v1_webhooks_webhook_id_deliveries,
    aws_api_gateway_integration.options_v1_webhooks_webhook_id_deliveries,
    # /v1/search
    aws_api_gateway_integration.get_v1_search,
    aws_api_gateway_integration.options_v1_search,
  ]

  triggers = {
    redeployment = sha1(jsonencode([
      # /v1/org
      aws_api_gateway_resource.v1_org.id,
      aws_api_gateway_method.get_v1_org.id,
      aws_api_gateway_integration.get_v1_org.id,
      aws_api_gateway_method.patch_v1_org.id,
      aws_api_gateway_integration.patch_v1_org.id,
      aws_api_gateway_method.options_v1_org.id,
      aws_api_gateway_integration.options_v1_org.id,
      # /v1/pods
      aws_api_gateway_resource.v1_pods.id,
      aws_api_gateway_method.get_v1_pods.id,
      aws_api_gateway_integration.get_v1_pods.id,
      aws_api_gateway_method.post_v1_pods.id,
      aws_api_gateway_integration.post_v1_pods.id,
      aws_api_gateway_method.options_v1_pods.id,
      aws_api_gateway_integration.options_v1_pods.id,
      # /v1/pods/{podId}
      aws_api_gateway_resource.v1_pods_pod_id.id,
      aws_api_gateway_method.get_v1_pods_pod_id.id,
      aws_api_gateway_integration.get_v1_pods_pod_id.id,
      aws_api_gateway_method.patch_v1_pods_pod_id.id,
      aws_api_gateway_integration.patch_v1_pods_pod_id.id,
      aws_api_gateway_method.delete_v1_pods_pod_id.id,
      aws_api_gateway_integration.delete_v1_pods_pod_id.id,
      aws_api_gateway_method.options_v1_pods_pod_id.id,
      aws_api_gateway_integration.options_v1_pods_pod_id.id,
      # /v1/keys
      aws_api_gateway_resource.v1_keys.id,
      aws_api_gateway_method.get_v1_keys.id,
      aws_api_gateway_integration.get_v1_keys.id,
      aws_api_gateway_method.post_v1_keys.id,
      aws_api_gateway_integration.post_v1_keys.id,
      aws_api_gateway_method.options_v1_keys.id,
      aws_api_gateway_integration.options_v1_keys.id,
      # /v1/keys/{keyId}
      aws_api_gateway_resource.v1_keys_key_id.id,
      aws_api_gateway_method.get_v1_keys_key_id.id,
      aws_api_gateway_integration.get_v1_keys_key_id.id,
      aws_api_gateway_method.delete_v1_keys_key_id.id,
      aws_api_gateway_integration.delete_v1_keys_key_id.id,
      aws_api_gateway_method.options_v1_keys_key_id.id,
      aws_api_gateway_integration.options_v1_keys_key_id.id,
      # /v1/inboxes
      aws_api_gateway_resource.v1_inboxes.id,
      aws_api_gateway_method.get_v1_inboxes.id,
      aws_api_gateway_integration.get_v1_inboxes.id,
      aws_api_gateway_method.post_v1_inboxes.id,
      aws_api_gateway_integration.post_v1_inboxes.id,
      aws_api_gateway_method.options_v1_inboxes.id,
      aws_api_gateway_integration.options_v1_inboxes.id,
      # /v1/inboxes/{inboxId}
      aws_api_gateway_resource.v1_inboxes_inbox_id.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id.id,
      aws_api_gateway_method.patch_v1_inboxes_inbox_id.id,
      aws_api_gateway_integration.patch_v1_inboxes_inbox_id.id,
      aws_api_gateway_method.delete_v1_inboxes_inbox_id.id,
      aws_api_gateway_integration.delete_v1_inboxes_inbox_id.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id.id,
      # /v1/inboxes/{inboxId}/threads
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_threads.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_threads.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads.id,
      # /v1/inboxes/{inboxId}/threads/{threadId}
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_method.patch_v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_integration.patch_v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id.id,
      # /v1/inboxes/{inboxId}/threads/{threadId}/labels/{labelId}
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels.id,
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_method.put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_integration.put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_method.delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_integration.delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id,
      # /v1/inboxes/{inboxId}/threads/{threadId}/messages
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id_messages.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id_messages.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_messages.id,
      # /v1/inboxes/{inboxId}/threads/{threadId}/messages/{messageId}
      aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id,
      # /v1/inboxes/{inboxId}/messages/send
      aws_api_gateway_resource.v1_inboxes_inbox_id_messages.id,
      aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id,
      aws_api_gateway_method.post_v1_inboxes_inbox_id_messages_send.id,
      aws_api_gateway_integration.post_v1_inboxes_inbox_id_messages_send.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_messages_send.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_messages_send.id,
      # /v1/inboxes/{inboxId}/drafts
      aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts.id,
      # /v1/inboxes/{inboxId}/drafts/{draftId}
      aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_method.get_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_integration.get_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_method.patch_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_integration.patch_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_method.delete_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_integration.delete_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id.id,
      # /v1/inboxes/{inboxId}/drafts/{draftId}/approve
      aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id,
      aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts_draft_id_approve.id,
      aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts_draft_id_approve.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_approve.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id_approve.id,
      # /v1/inboxes/{inboxId}/drafts/{draftId}/reject
      aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id,
      aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts_draft_id_reject.id,
      aws_api_gateway_integration.post_v1_inboxes_inbox_id_drafts_draft_id_reject.id,
      aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_reject.id,
      aws_api_gateway_integration.options_v1_inboxes_inbox_id_drafts_draft_id_reject.id,
      # /v1/labels
      aws_api_gateway_resource.v1_labels.id,
      aws_api_gateway_method.get_v1_labels.id,
      aws_api_gateway_integration.get_v1_labels.id,
      aws_api_gateway_method.post_v1_labels.id,
      aws_api_gateway_integration.post_v1_labels.id,
      aws_api_gateway_method.options_v1_labels.id,
      aws_api_gateway_integration.options_v1_labels.id,
      # /v1/labels/{labelId}
      aws_api_gateway_resource.v1_labels_label_id.id,
      aws_api_gateway_method.patch_v1_labels_label_id.id,
      aws_api_gateway_integration.patch_v1_labels_label_id.id,
      aws_api_gateway_method.delete_v1_labels_label_id.id,
      aws_api_gateway_integration.delete_v1_labels_label_id.id,
      aws_api_gateway_method.options_v1_labels_label_id.id,
      aws_api_gateway_integration.options_v1_labels_label_id.id,
      # /v1/webhooks
      aws_api_gateway_resource.v1_webhooks.id,
      aws_api_gateway_method.get_v1_webhooks.id,
      aws_api_gateway_integration.get_v1_webhooks.id,
      aws_api_gateway_method.post_v1_webhooks.id,
      aws_api_gateway_integration.post_v1_webhooks.id,
      aws_api_gateway_method.options_v1_webhooks.id,
      aws_api_gateway_integration.options_v1_webhooks.id,
      # /v1/webhooks/{webhookId}
      aws_api_gateway_resource.v1_webhooks_webhook_id.id,
      aws_api_gateway_method.get_v1_webhooks_webhook_id.id,
      aws_api_gateway_integration.get_v1_webhooks_webhook_id.id,
      aws_api_gateway_method.patch_v1_webhooks_webhook_id.id,
      aws_api_gateway_integration.patch_v1_webhooks_webhook_id.id,
      aws_api_gateway_method.delete_v1_webhooks_webhook_id.id,
      aws_api_gateway_integration.delete_v1_webhooks_webhook_id.id,
      aws_api_gateway_method.options_v1_webhooks_webhook_id.id,
      aws_api_gateway_integration.options_v1_webhooks_webhook_id.id,
      # /v1/webhooks/{webhookId}/deliveries
      aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id,
      aws_api_gateway_method.get_v1_webhooks_webhook_id_deliveries.id,
      aws_api_gateway_integration.get_v1_webhooks_webhook_id_deliveries.id,
      aws_api_gateway_method.options_v1_webhooks_webhook_id_deliveries.id,
      aws_api_gateway_integration.options_v1_webhooks_webhook_id_deliveries.id,
      # /v1/search
      aws_api_gateway_resource.v1_search.id,
      aws_api_gateway_method.get_v1_search.id,
      aws_api_gateway_integration.get_v1_search.id,
      aws_api_gateway_method.options_v1_search.id,
      aws_api_gateway_integration.options_v1_search.id,
      # /v1/domains
      aws_api_gateway_resource.v1_domains.id,
      aws_api_gateway_resource.v1_domains_domain_id.id,
      aws_api_gateway_resource.v1_domains_domain_id_verify.id,
      aws_api_gateway_method.post_v1_domains.id,
      aws_api_gateway_integration.post_v1_domains.id,
      aws_api_gateway_method.get_v1_domains.id,
      aws_api_gateway_integration.get_v1_domains.id,
      aws_api_gateway_method.options_v1_domains.id,
      aws_api_gateway_integration.options_v1_domains.id,
      aws_api_gateway_method.get_v1_domains_domain_id.id,
      aws_api_gateway_integration.get_v1_domains_domain_id.id,
      aws_api_gateway_method.delete_v1_domains_domain_id.id,
      aws_api_gateway_integration.delete_v1_domains_domain_id.id,
      aws_api_gateway_method.options_v1_domains_domain_id.id,
      aws_api_gateway_integration.options_v1_domains_domain_id.id,
      aws_api_gateway_method.post_v1_domains_domain_id_verify.id,
      aws_api_gateway_integration.post_v1_domains_domain_id_verify.id,
      aws_api_gateway_method.options_v1_domains_domain_id_verify.id,
      aws_api_gateway_integration.options_v1_domains_domain_id_verify.id,
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

# ── Stage ─────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_stage" "main" {
  deployment_id = aws_api_gateway_deployment.main.id
  rest_api_id   = aws_api_gateway_rest_api.main.id
  stage_name    = var.environment

  xray_tracing_enabled = true

  depends_on = [aws_api_gateway_account.main]

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      ip             = "$context.identity.sourceIp"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      routeKey       = "$context.routeKey"
      status         = "$context.status"
      responseLength = "$context.responseLength"
      errorMessage   = "$context.error.message"
    })
  }

}

resource "aws_api_gateway_method_settings" "main" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  stage_name  = aws_api_gateway_stage.main.stage_name
  method_path = "*/*"

  settings {
    logging_level      = "INFO"
    metrics_enabled    = true
    data_trace_enabled = false
  }
}

# ── Usage Plan ────────────────────────────────────────────────────────────────

resource "aws_api_gateway_usage_plan" "main" {
  name = "${local.prefix}-usage-plan"

  throttle_settings {
    rate_limit  = 100
    burst_limit = 200
  }

  quota_settings {
    limit  = 10000
    period = "DAY"
  }

  api_stages {
    api_id = aws_api_gateway_rest_api.main.id
    stage  = aws_api_gateway_stage.main.stage_name
  }
}

# ── CloudWatch Log Group ──────────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "api_gateway" {
  name              = "/aws/apigateway/${local.prefix}"
  retention_in_days = 30
}

# ── Lambda Permission: API Gateway → Authorizer ───────────────────────────────

resource "aws_lambda_permission" "api_gateway_authorizer" {
  statement_id  = "AllowAPIGatewayInvokeAuthorizer"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.authorizer.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}
