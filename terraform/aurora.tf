# ── DB Subnet Group ───────────────────────────────────────────────────────────

resource "aws_db_subnet_group" "main" {
  name       = "${local.prefix}-db-subnet-group"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

# ── Cluster Parameter Group ───────────────────────────────────────────────────

resource "aws_rds_cluster_parameter_group" "main" {
  name   = "${local.prefix}-aurora-pg16"
  family = "aurora-postgresql16"

  parameter {
    name  = "shared_preload_libraries"
    value = "pg_stat_statements"
  }
}

# ── Aurora Serverless v2 Cluster ──────────────────────────────────────────────

resource "aws_rds_cluster" "main" {
  cluster_identifier = "${local.prefix}-cluster"

  engine         = "aurora-postgresql"
  engine_version = "16.4"
  engine_mode    = "provisioned"

  serverlessv2_scaling_configuration {
    min_capacity = var.aurora_min_capacity
    max_capacity = var.aurora_max_capacity
  }

  database_name                   = var.db_name
  master_username                 = var.db_username
  master_password                 = random_password.db_password.result
  db_subnet_group_name            = aws_db_subnet_group.main.name
  vpc_security_group_ids          = [aws_security_group.aurora.id]
  db_cluster_parameter_group_name = aws_rds_cluster_parameter_group.main.name

  storage_encrypted = true

  skip_final_snapshot       = false
  final_snapshot_identifier = "${local.prefix}-final-snapshot"
  deletion_protection       = true

  backup_retention_period      = 7
  preferred_backup_window      = "03:00-04:00"
  preferred_maintenance_window = "sun:04:00-sun:05:00"

  enabled_cloudwatch_logs_exports = ["postgresql"]

  depends_on = [aws_secretsmanager_secret_version.db_password]
}

# ── Cluster Instance (single writer — Serverless v2 auto-scales) ──────────────

resource "aws_rds_cluster_instance" "main" {
  count = 1

  identifier         = "${local.prefix}-instance-${count.index}"
  cluster_identifier = aws_rds_cluster.main.id
  instance_class     = "db.serverless"

  engine         = aws_rds_cluster.main.engine
  engine_version = aws_rds_cluster.main.engine_version

  db_subnet_group_name = aws_db_subnet_group.main.name
  publicly_accessible  = false
}

# ── IAM Role for RDS Proxy ────────────────────────────────────────────────────

resource "aws_iam_role" "rds_proxy" {
  name = "${local.prefix}-rds-proxy-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "rds.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })
}

resource "aws_iam_role_policy" "rds_proxy_secrets" {
  name = "${local.prefix}-rds-proxy-secrets"
  role = aws_iam_role.rds_proxy.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = aws_secretsmanager_secret.db_password.arn
      }
    ]
  })
}

# ── RDS Proxy ─────────────────────────────────────────────────────────────────

resource "aws_db_proxy" "main" {
  name                   = "${local.prefix}-proxy"
  debug_logging          = false
  engine_family          = "POSTGRESQL"
  idle_client_timeout    = 1800
  require_tls            = true
  role_arn               = aws_iam_role.rds_proxy.arn
  vpc_subnet_ids         = [aws_subnet.private_a.id, aws_subnet.private_b.id]
  vpc_security_group_ids = [aws_security_group.rds_proxy.id]

  auth {
    auth_scheme = "SECRETS"
    secret_arn  = aws_secretsmanager_secret.db_password.arn
    iam_auth    = "DISABLED"
  }
}

# ── Proxy Default Target Group ────────────────────────────────────────────────

resource "aws_db_proxy_default_target_group" "main" {
  db_proxy_name = aws_db_proxy.main.name

  connection_pool_config {
    max_connections_percent      = 100
    connection_borrow_timeout    = 120
  }
}

# ── Proxy Target ──────────────────────────────────────────────────────────────

resource "aws_db_proxy_target" "main" {
  db_proxy_name          = aws_db_proxy.main.name
  target_group_name      = aws_db_proxy_default_target_group.main.name
  db_cluster_identifier  = aws_rds_cluster.main.id
}
