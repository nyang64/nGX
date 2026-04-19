# ── WebSocket API ─────────────────────────────────────────────────────────────

resource "aws_apigatewayv2_api" "websocket" {
  name                       = "${local.prefix}-websocket"
  protocol_type              = "WEBSOCKET"
  route_selection_expression = "$request.body.action"
}

# ── Authorizer ────────────────────────────────────────────────────────────────

resource "aws_apigatewayv2_authorizer" "ws" {
  api_id           = aws_apigatewayv2_api.websocket.id
  authorizer_type  = "REQUEST"
  authorizer_uri   = aws_lambda_function.authorizer.invoke_arn
  name             = "${local.prefix}-ws-authorizer"
  identity_sources = ["route.request.querystring.token"]
}

# ── Lambda Permission: WebSocket API → Authorizer ─────────────────────────────

resource "aws_lambda_permission" "ws_authorizer" {
  statement_id  = "AllowWebSocketAPIGatewayInvokeAuthorizer"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.authorizer.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*"
}

# ── Integrations ──────────────────────────────────────────────────────────────

resource "aws_apigatewayv2_integration" "ws_connect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.ws_connect.invoke_arn
  integration_method = "POST"
}

resource "aws_apigatewayv2_integration" "ws_disconnect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  integration_type   = "AWS_PROXY"
  integration_uri    = aws_lambda_function.ws_disconnect.invoke_arn
  integration_method = "POST"
}

# ── Lambda Permissions: WebSocket API → Handlers ──────────────────────────────

resource "aws_lambda_permission" "ws_connect_api" {
  statement_id  = "AllowWebSocketAPIGatewayInvokeConnect"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_connect.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*"
}

resource "aws_lambda_permission" "ws_disconnect_api" {
  statement_id  = "AllowWebSocketAPIGatewayInvokeDisconnect"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.ws_disconnect.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.websocket.execution_arn}/*"
}

# ── Routes ────────────────────────────────────────────────────────────────────

resource "aws_apigatewayv2_route" "connect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  route_key          = "$connect"
  authorization_type = "CUSTOM"
  authorizer_id      = aws_apigatewayv2_authorizer.ws.id
  target             = "integrations/${aws_apigatewayv2_integration.ws_connect.id}"
}

resource "aws_apigatewayv2_route" "disconnect" {
  api_id             = aws_apigatewayv2_api.websocket.id
  route_key          = "$disconnect"
  authorization_type = "NONE"
  target             = "integrations/${aws_apigatewayv2_integration.ws_disconnect.id}"
}

# ── Stage ─────────────────────────────────────────────────────────────────────

resource "aws_apigatewayv2_stage" "websocket" {
  api_id      = aws_apigatewayv2_api.websocket.id
  name        = var.environment
  auto_deploy = true

  default_route_settings {
    logging_level          = "INFO"
    data_trace_enabled     = false
    throttling_rate_limit  = 100
    throttling_burst_limit = 200
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.websocket_api.arn
    format = jsonencode({
      requestId   = "$context.requestId"
      ip          = "$context.identity.sourceIp"
      requestTime = "$context.requestTime"
      routeKey    = "$context.routeKey"
      status      = "$context.status"
      errorMessage = "$context.error.message"
    })
  }
}

# ── CloudWatch Log Group ──────────────────────────────────────────────────────

resource "aws_cloudwatch_log_group" "websocket_api" {
  name              = "/aws/apigateway/${local.prefix}-websocket"
  retention_in_days = 30
}
