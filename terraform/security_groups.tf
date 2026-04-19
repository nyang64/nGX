# ── Lambda Security Group ─────────────────────────────────────────────────────
# Applied to all Lambda functions. No inbound rules — Lambda is invoked
# via API Gateway or SQS event source mappings, not by inbound TCP connections.
# Unrestricted egress allows Lambda to reach AWS service endpoints (SQS, SNS,
# S3, API Gateway management) and the RDS Proxy.

resource "aws_security_group" "lambda" {
  name        = "${local.prefix}-lambda"
  description = "Attached to all Lambda functions - egress-only, no inbound TCP"
  vpc_id      = aws_vpc.main.id

  egress {
    description = "Allow all outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ── RDS Proxy Security Group ──────────────────────────────────────────────────
# Accepts PostgreSQL connections from Lambda only.
# Egress is unrestricted so the proxy can reach the Aurora cluster.

resource "aws_security_group" "rds_proxy" {
  name        = "${local.prefix}-rds-proxy"
  description = "RDS Proxy - accepts Postgres from Lambda SG, egress to Aurora"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "PostgreSQL from Lambda"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.lambda.id]
  }

  egress {
    description = "Allow all outbound traffic"
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ── Aurora Security Group ─────────────────────────────────────────────────────
# Accepts PostgreSQL connections from the RDS Proxy only.
# No egress rule — Aurora does not initiate outbound connections.

resource "aws_security_group" "aurora" {
  name        = "${local.prefix}-aurora"
  description = "Aurora cluster - accepts Postgres from RDS Proxy SG only"
  vpc_id      = aws_vpc.main.id

  ingress {
    description     = "PostgreSQL from RDS Proxy"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.rds_proxy.id]
  }
}
