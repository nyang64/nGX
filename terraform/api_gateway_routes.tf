# ── Lambda Integration URI Locals ─────────────────────────────────────────────

locals {
  lambda_uri = {
    orgs     = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.orgs.arn}/invocations"
    auth     = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.auth.arn}/invocations"
    inboxes  = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.inboxes.arn}/invocations"
    threads  = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.threads.arn}/invocations"
    messages = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.messages.arn}/invocations"
    drafts   = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.drafts.arn}/invocations"
    webhooks = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.webhooks.arn}/invocations"
    search   = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.search.arn}/invocations"
    domains  = "arn:aws:apigateway:${var.aws_region}:lambda:path/2015-03-31/functions/${aws_lambda_function.domains.arn}/invocations"
  }

  cors_response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = "'Content-Type,Authorization,X-Amz-Date,X-Api-Key,X-Amz-Security-Token'"
    "method.response.header.Access-Control-Allow-Methods" = "'GET,POST,PUT,PATCH,DELETE,OPTIONS'"
    "method.response.header.Access-Control-Allow-Origin"  = "'*'"
  }
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_rest_api.main.root_resource_id
  path_part   = "v1"
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/org
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_org" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "org"
}

resource "aws_api_gateway_method" "get_v1_org" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_org.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_org" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_org.id
  http_method             = aws_api_gateway_method.get_v1_org.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "patch_v1_org" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_org.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_org" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_org.id
  http_method             = aws_api_gateway_method.patch_v1_org.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "options_v1_org" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_org.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_org" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_org.id
  http_method = aws_api_gateway_method.options_v1_org.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_org" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_org.id
  http_method = aws_api_gateway_method.options_v1_org.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_org" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_org.id
  http_method         = aws_api_gateway_method.options_v1_org.http_method
  status_code         = aws_api_gateway_method_response.options_v1_org.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "orgs_api" {
  statement_id  = "AllowAPIGatewayInvokeOrgs"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.orgs.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/pods
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_pods" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "pods"
}

resource "aws_api_gateway_method" "get_v1_pods" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_pods" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_pods.id
  http_method             = aws_api_gateway_method.get_v1_pods.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "post_v1_pods" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_pods" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_pods.id
  http_method             = aws_api_gateway_method.post_v1_pods.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "options_v1_pods" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_pods" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_pods.id
  http_method = aws_api_gateway_method.options_v1_pods.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_pods" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_pods.id
  http_method = aws_api_gateway_method.options_v1_pods.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_pods" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_pods.id
  http_method         = aws_api_gateway_method.options_v1_pods.http_method
  status_code         = aws_api_gateway_method_response.options_v1_pods.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/pods/{podId} ──────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_pods_pod_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_pods.id
  path_part   = "{podId}"
}

resource "aws_api_gateway_method" "get_v1_pods_pod_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_pods_pod_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method             = aws_api_gateway_method.get_v1_pods_pod_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "patch_v1_pods_pod_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_pods_pod_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method             = aws_api_gateway_method.patch_v1_pods_pod_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "delete_v1_pods_pod_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_pods_pod_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method             = aws_api_gateway_method.delete_v1_pods_pod_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.orgs
}

resource "aws_api_gateway_method" "options_v1_pods_pod_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_pods_pod_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method = aws_api_gateway_method.options_v1_pods_pod_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_pods_pod_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method = aws_api_gateway_method.options_v1_pods_pod_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_pods_pod_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_pods_pod_id.id
  http_method         = aws_api_gateway_method.options_v1_pods_pod_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_pods_pod_id.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/keys
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_keys" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "keys"
}

resource "aws_api_gateway_method" "get_v1_keys" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_keys" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_keys.id
  http_method             = aws_api_gateway_method.get_v1_keys.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.auth
}

resource "aws_api_gateway_method" "post_v1_keys" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_keys" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_keys.id
  http_method             = aws_api_gateway_method.post_v1_keys.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.auth
}

resource "aws_api_gateway_method" "options_v1_keys" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_keys" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_keys.id
  http_method = aws_api_gateway_method.options_v1_keys.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_keys" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_keys.id
  http_method = aws_api_gateway_method.options_v1_keys.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_keys" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_keys.id
  http_method         = aws_api_gateway_method.options_v1_keys.http_method
  status_code         = aws_api_gateway_method_response.options_v1_keys.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "auth_api" {
  statement_id  = "AllowAPIGatewayInvokeAuth"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.auth.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/keys/{keyId} ──────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_keys_key_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_keys.id
  path_part   = "{keyId}"
}

resource "aws_api_gateway_method" "get_v1_keys_key_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys_key_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_keys_key_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_keys_key_id.id
  http_method             = aws_api_gateway_method.get_v1_keys_key_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.auth
}

resource "aws_api_gateway_method" "delete_v1_keys_key_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys_key_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_keys_key_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_keys_key_id.id
  http_method             = aws_api_gateway_method.delete_v1_keys_key_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.auth
}

resource "aws_api_gateway_method" "options_v1_keys_key_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_keys_key_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_keys_key_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_keys_key_id.id
  http_method = aws_api_gateway_method.options_v1_keys_key_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_keys_key_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_keys_key_id.id
  http_method = aws_api_gateway_method.options_v1_keys_key_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_keys_key_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_keys_key_id.id
  http_method         = aws_api_gateway_method.options_v1_keys_key_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_keys_key_id.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/inboxes
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "inboxes"
}

resource "aws_api_gateway_method" "get_v1_inboxes" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes.id
  http_method             = aws_api_gateway_method.get_v1_inboxes.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.inboxes
}

resource "aws_api_gateway_method" "post_v1_inboxes" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_inboxes" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes.id
  http_method             = aws_api_gateway_method.post_v1_inboxes.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.inboxes
}

resource "aws_api_gateway_method" "options_v1_inboxes" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes.id
  http_method = aws_api_gateway_method.options_v1_inboxes.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes.id
  http_method = aws_api_gateway_method.options_v1_inboxes.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes.id
  http_method         = aws_api_gateway_method.options_v1_inboxes.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "inboxes_api" {
  statement_id  = "AllowAPIGatewayInvokeInboxes"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.inboxes.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/inboxes/{inboxId} ────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes.id
  path_part   = "{inboxId}"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.inboxes
}

resource "aws_api_gateway_method" "patch_v1_inboxes_inbox_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_inboxes_inbox_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method             = aws_api_gateway_method.patch_v1_inboxes_inbox_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.inboxes
}

resource "aws_api_gateway_method" "delete_v1_inboxes_inbox_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_inboxes_inbox_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method             = aws_api_gateway_method.delete_v1_inboxes_inbox_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.inboxes
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/inboxes/{inboxId}/threads
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  path_part   = "threads"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_threads" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_threads" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_threads.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_threads" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_threads" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_threads" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_threads" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_threads.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "threads_api" {
  statement_id  = "AllowAPIGatewayInvokeThreads"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.threads.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/inboxes/{inboxId}/threads/{threadId} ──────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads.id
  path_part   = "{threadId}"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "patch_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method             = aws_api_gateway_method.patch_v1_inboxes_inbox_id_threads_thread_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_threads_thread_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_threads_thread_id.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/inboxes/{inboxId}/threads/{threadId}/labels ───────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads_thread_id_labels" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  path_part   = "labels"
}

# ── /v1/inboxes/{inboxId}/threads/{threadId}/labels/{labelId} ─────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels.id
  path_part   = "{labelId}"
}

resource "aws_api_gateway_method" "put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method   = "PUT"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method             = aws_api_gateway_method.put_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method             = aws_api_gateway_method.delete_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_labels_label_id.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_threads_thread_id_labels_label_id.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/inboxes/{inboxId}/threads/{threadId}/messages ─────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id.id
  path_part   = "messages"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id_messages.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.messages
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_threads_thread_id_messages" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_threads_thread_id_messages.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "messages_api" {
  statement_id  = "AllowAPIGatewayInvokeMessages"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.messages.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/inboxes/{inboxId}/threads/{threadId}/messages/{messageId} ─────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages.id
  path_part   = "{messageId}"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.messages
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_threads_thread_id_messages_message_id.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_threads_thread_id_messages_message_id.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/inboxes/{inboxId}/messages
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_messages" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  path_part   = "messages"
}

# ── /v1/inboxes/{inboxId}/messages/send ──────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_messages_send" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_messages.id
  path_part   = "send"
}

resource "aws_api_gateway_method" "post_v1_inboxes_inbox_id_messages_send" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_inboxes_inbox_id_messages_send" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method             = aws_api_gateway_method.post_v1_inboxes_inbox_id_messages_send.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.messages
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_messages_send" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_messages_send" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_messages_send.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_messages_send" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_messages_send.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_messages_send" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_messages_send.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_messages_send.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_messages_send.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/inboxes/{inboxId}/drafts
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_drafts" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id.id
  path_part   = "drafts"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_drafts" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_drafts" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_drafts.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "post_v1_inboxes_inbox_id_drafts" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_inboxes_inbox_id_drafts" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method             = aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_drafts" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_drafts" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_drafts" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_drafts" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_drafts.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "drafts_api" {
  statement_id  = "AllowAPIGatewayInvokeDrafts"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.drafts.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/inboxes/{inboxId}/drafts/{draftId} ────────────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts.id
  path_part   = "{draftId}"
}

resource "aws_api_gateway_method" "get_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method             = aws_api_gateway_method.get_v1_inboxes_inbox_id_drafts_draft_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "patch_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method             = aws_api_gateway_method.patch_v1_inboxes_inbox_id_drafts_draft_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "delete_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method             = aws_api_gateway_method.delete_v1_inboxes_inbox_id_drafts_draft_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_drafts_draft_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_drafts_draft_id.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/inboxes/{inboxId}/drafts/{draftId}/approve ───────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  path_part   = "approve"
}

resource "aws_api_gateway_method" "post_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method             = aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts_draft_id_approve.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_approve.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_approve.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_drafts_draft_id_approve" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_approve.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_approve.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_drafts_draft_id_approve.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/inboxes/{inboxId}/drafts/{draftId}/reject ────────────────────────────

resource "aws_api_gateway_resource" "v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id.id
  path_part   = "reject"
}

resource "aws_api_gateway_method" "post_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method             = aws_api_gateway_method.post_v1_inboxes_inbox_id_drafts_draft_id_reject.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.drafts
}

resource "aws_api_gateway_method" "options_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_reject.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_reject.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_inboxes_inbox_id_drafts_draft_id_reject" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_inboxes_inbox_id_drafts_draft_id_reject.id
  http_method         = aws_api_gateway_method.options_v1_inboxes_inbox_id_drafts_draft_id_reject.http_method
  status_code         = aws_api_gateway_method_response.options_v1_inboxes_inbox_id_drafts_draft_id_reject.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/labels
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_labels" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "labels"
}

resource "aws_api_gateway_method" "get_v1_labels" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_labels" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_labels.id
  http_method             = aws_api_gateway_method.get_v1_labels.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "post_v1_labels" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_labels" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_labels.id
  http_method             = aws_api_gateway_method.post_v1_labels.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "options_v1_labels" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_labels" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_labels.id
  http_method = aws_api_gateway_method.options_v1_labels.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_labels" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_labels.id
  http_method = aws_api_gateway_method.options_v1_labels.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_labels" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_labels.id
  http_method         = aws_api_gateway_method.options_v1_labels.http_method
  status_code         = aws_api_gateway_method_response.options_v1_labels.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/labels/{labelId} ──────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_labels.id
  path_part   = "{labelId}"
}

resource "aws_api_gateway_method" "patch_v1_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels_label_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_labels_label_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_labels_label_id.id
  http_method             = aws_api_gateway_method.patch_v1_labels_label_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "delete_v1_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels_label_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_labels_label_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_labels_label_id.id
  http_method             = aws_api_gateway_method.delete_v1_labels_label_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.threads
}

resource "aws_api_gateway_method" "options_v1_labels_label_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_labels_label_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_labels_label_id.id
  http_method = aws_api_gateway_method.options_v1_labels_label_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_labels_label_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_labels_label_id.id
  http_method = aws_api_gateway_method.options_v1_labels_label_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_labels_label_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_labels_label_id.id
  http_method         = aws_api_gateway_method.options_v1_labels_label_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_labels_label_id.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/webhooks
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_webhooks" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "webhooks"
}

resource "aws_api_gateway_method" "get_v1_webhooks" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_webhooks" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks.id
  http_method             = aws_api_gateway_method.get_v1_webhooks.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "post_v1_webhooks" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_webhooks" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks.id
  http_method             = aws_api_gateway_method.post_v1_webhooks.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "options_v1_webhooks" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_webhooks" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks.id
  http_method = aws_api_gateway_method.options_v1_webhooks.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_webhooks" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks.id
  http_method = aws_api_gateway_method.options_v1_webhooks.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_webhooks" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_webhooks.id
  http_method         = aws_api_gateway_method.options_v1_webhooks.http_method
  status_code         = aws_api_gateway_method_response.options_v1_webhooks.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "webhooks_api" {
  statement_id  = "AllowAPIGatewayInvokeWebhooks"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.webhooks.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ── /v1/webhooks/{webhookId} ──────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_webhooks_webhook_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_webhooks.id
  path_part   = "{webhookId}"
}

resource "aws_api_gateway_method" "get_v1_webhooks_webhook_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_webhooks_webhook_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method             = aws_api_gateway_method.get_v1_webhooks_webhook_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "patch_v1_webhooks_webhook_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method   = "PATCH"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "patch_v1_webhooks_webhook_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method             = aws_api_gateway_method.patch_v1_webhooks_webhook_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "delete_v1_webhooks_webhook_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_webhooks_webhook_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method             = aws_api_gateway_method.delete_v1_webhooks_webhook_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "options_v1_webhooks_webhook_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_webhooks_webhook_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method = aws_api_gateway_method.options_v1_webhooks_webhook_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_webhooks_webhook_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method = aws_api_gateway_method.options_v1_webhooks_webhook_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_webhooks_webhook_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  http_method         = aws_api_gateway_method.options_v1_webhooks_webhook_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_webhooks_webhook_id.status_code
  response_parameters = local.cors_response_parameters
}

# ── /v1/webhooks/{webhookId}/deliveries ───────────────────────────────────────

resource "aws_api_gateway_resource" "v1_webhooks_webhook_id_deliveries" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_webhooks_webhook_id.id
  path_part   = "deliveries"
}

resource "aws_api_gateway_method" "get_v1_webhooks_webhook_id_deliveries" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_webhooks_webhook_id_deliveries" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method             = aws_api_gateway_method.get_v1_webhooks_webhook_id_deliveries.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.webhooks
}

resource "aws_api_gateway_method" "options_v1_webhooks_webhook_id_deliveries" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_webhooks_webhook_id_deliveries" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method = aws_api_gateway_method.options_v1_webhooks_webhook_id_deliveries.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_webhooks_webhook_id_deliveries" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method = aws_api_gateway_method.options_v1_webhooks_webhook_id_deliveries.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_webhooks_webhook_id_deliveries" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_webhooks_webhook_id_deliveries.id
  http_method         = aws_api_gateway_method.options_v1_webhooks_webhook_id_deliveries.http_method
  status_code         = aws_api_gateway_method_response.options_v1_webhooks_webhook_id_deliveries.status_code
  response_parameters = local.cors_response_parameters
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/search
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_search" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "search"
}

resource "aws_api_gateway_method" "get_v1_search" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_search.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_search" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_search.id
  http_method             = aws_api_gateway_method.get_v1_search.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.search
}

resource "aws_api_gateway_method" "options_v1_search" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_search.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_search" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_search.id
  http_method = aws_api_gateway_method.options_v1_search.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = "{\"statusCode\": 200}"
  }
}

resource "aws_api_gateway_method_response" "options_v1_search" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_search.id
  http_method = aws_api_gateway_method.options_v1_search.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_search" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_search.id
  http_method         = aws_api_gateway_method.options_v1_search.http_method
  status_code         = aws_api_gateway_method_response.options_v1_search.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "search_api" {
  statement_id  = "AllowAPIGatewayInvokeSearch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.search.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}

# ─────────────────────────────────────────────────────────────────────────────
# /v1/domains  — custom domain management (enterprise BYOD)
# ─────────────────────────────────────────────────────────────────────────────

resource "aws_api_gateway_resource" "v1_domains" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1.id
  path_part   = "domains"
}

resource "aws_api_gateway_resource" "v1_domains_domain_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_domains.id
  path_part   = "{domain_id}"
}

resource "aws_api_gateway_resource" "v1_domains_domain_id_verify" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  parent_id   = aws_api_gateway_resource.v1_domains_domain_id.id
  path_part   = "verify"
}

# POST /v1/domains
resource "aws_api_gateway_method" "post_v1_domains" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_domains" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_domains.id
  http_method             = aws_api_gateway_method.post_v1_domains.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.domains
}

# GET /v1/domains
resource "aws_api_gateway_method" "get_v1_domains" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_domains" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_domains.id
  http_method             = aws_api_gateway_method.get_v1_domains.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.domains
}

# OPTIONS /v1/domains  (CORS)
resource "aws_api_gateway_method" "options_v1_domains" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_domains" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains.id
  http_method = aws_api_gateway_method.options_v1_domains.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = jsonencode({ statusCode = 200 })
  }
}

resource "aws_api_gateway_method_response" "options_v1_domains" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains.id
  http_method = aws_api_gateway_method.options_v1_domains.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_domains" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_domains.id
  http_method         = aws_api_gateway_method.options_v1_domains.http_method
  status_code         = aws_api_gateway_method_response.options_v1_domains.status_code
  response_parameters = local.cors_response_parameters
}

# GET /v1/domains/{domain_id}
resource "aws_api_gateway_method" "get_v1_domains_domain_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method   = "GET"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "get_v1_domains_domain_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method             = aws_api_gateway_method.get_v1_domains_domain_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.domains
  timeout_milliseconds    = 29000
}

# DELETE /v1/domains/{domain_id}
resource "aws_api_gateway_method" "delete_v1_domains_domain_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method   = "DELETE"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "delete_v1_domains_domain_id" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method             = aws_api_gateway_method.delete_v1_domains_domain_id.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.domains
  timeout_milliseconds    = 29000
}

# OPTIONS /v1/domains/{domain_id}  (CORS)
resource "aws_api_gateway_method" "options_v1_domains_domain_id" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_domains_domain_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method = aws_api_gateway_method.options_v1_domains_domain_id.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = jsonencode({ statusCode = 200 })
  }
}

resource "aws_api_gateway_method_response" "options_v1_domains_domain_id" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method = aws_api_gateway_method.options_v1_domains_domain_id.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_domains_domain_id" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_domains_domain_id.id
  http_method         = aws_api_gateway_method.options_v1_domains_domain_id.http_method
  status_code         = aws_api_gateway_method_response.options_v1_domains_domain_id.status_code
  response_parameters = local.cors_response_parameters
}

# POST /v1/domains/{domain_id}/verify
resource "aws_api_gateway_method" "post_v1_domains_domain_id_verify" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method   = "POST"
  authorization = "CUSTOM"
  authorizer_id = aws_api_gateway_authorizer.api_key.id
}

resource "aws_api_gateway_integration" "post_v1_domains_domain_id_verify" {
  rest_api_id             = aws_api_gateway_rest_api.main.id
  resource_id             = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method             = aws_api_gateway_method.post_v1_domains_domain_id_verify.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = local.lambda_uri.domains
  timeout_milliseconds    = 29000
}

# OPTIONS /v1/domains/{domain_id}/verify  (CORS)
resource "aws_api_gateway_method" "options_v1_domains_domain_id_verify" {
  rest_api_id   = aws_api_gateway_rest_api.main.id
  resource_id   = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method   = "OPTIONS"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "options_v1_domains_domain_id_verify" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method = aws_api_gateway_method.options_v1_domains_domain_id_verify.http_method
  type        = "MOCK"
  request_templates = {
    "application/json" = jsonencode({ statusCode = 200 })
  }
}

resource "aws_api_gateway_method_response" "options_v1_domains_domain_id_verify" {
  rest_api_id = aws_api_gateway_rest_api.main.id
  resource_id = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method = aws_api_gateway_method.options_v1_domains_domain_id_verify.http_method
  status_code = "200"
  response_parameters = {
    "method.response.header.Access-Control-Allow-Headers" = true
    "method.response.header.Access-Control-Allow-Methods" = true
    "method.response.header.Access-Control-Allow-Origin"  = true
  }
}

resource "aws_api_gateway_integration_response" "options_v1_domains_domain_id_verify" {
  rest_api_id         = aws_api_gateway_rest_api.main.id
  resource_id         = aws_api_gateway_resource.v1_domains_domain_id_verify.id
  http_method         = aws_api_gateway_method.options_v1_domains_domain_id_verify.http_method
  status_code         = aws_api_gateway_method_response.options_v1_domains_domain_id_verify.status_code
  response_parameters = local.cors_response_parameters
}

# Lambda permission
resource "aws_lambda_permission" "domains_api" {
  statement_id  = "AllowAPIGatewayInvokeDomains"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.domains.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.main.execution_arn}/*"
}
