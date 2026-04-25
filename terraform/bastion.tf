# ── Bastion Host ──────────────────────────────────────────────────────────────
# t2.micro (free tier eligible for 12 months).
# No SSH key — access via AWS SSM Session Manager only.
# Use for port-forwarding to RDS Proxy and Aurora (both private-subnet-only).

# ── SSM IAM Role ──────────────────────────────────────────────────────────────

resource "aws_iam_role" "bastion" {
  name = "${local.prefix}-bastion"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "bastion_ssm" {
  role       = aws_iam_role.bastion.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "bastion" {
  name = "${local.prefix}-bastion"
  role = aws_iam_role.bastion.name
}

# ── Security Group ────────────────────────────────────────────────────────────
# No inbound SSH — SSM connects without open ports.
# Egress: HTTPS for SSM, PostgreSQL to RDS Proxy.

resource "aws_security_group" "bastion" {
  name        = "${local.prefix}-bastion"
  description = "Bastion - SSM access only, no inbound SSH"
  vpc_id      = aws_vpc.main.id

  egress {
    description = "HTTPS for SSM agent"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  # PostgreSQL egress to RDS Proxy is managed via aws_security_group_rule below
  # to avoid a circular SG dependency (rds_proxy ingress also references bastion).
}

# ── Cross-SG rules (bastion ↔ RDS Proxy) ─────────────────────────────────────
# Using aws_security_group_rule resources for both directions breaks the cycle
# that would result from inline cross-SG egress/ingress blocks.

resource "aws_security_group_rule" "bastion_to_rds_proxy" {
  type                     = "egress"
  description              = "PostgreSQL to RDS Proxy"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = aws_security_group.bastion.id
  source_security_group_id = aws_security_group.rds_proxy.id
}

resource "aws_security_group_rule" "rds_proxy_from_bastion" {
  type                     = "ingress"
  description              = "PostgreSQL from bastion (SSM tunnel)"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = aws_security_group.rds_proxy.id
  source_security_group_id = aws_security_group.bastion.id
}

resource "aws_security_group_rule" "rds_proxy_from_lambda" {
  type                     = "ingress"
  description              = "PostgreSQL from Lambda"
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  security_group_id        = aws_security_group.rds_proxy.id
  source_security_group_id = aws_security_group.lambda.id
}

# ── AMI: latest Amazon Linux 2 ────────────────────────────────────────────────

data "aws_ami" "amazon_linux_2" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["amzn2-ami-hvm-2.0.*-x86_64-gp2"]
  }

  filter {
    name   = "state"
    values = ["available"]
  }
}

# ── EC2 Instance ──────────────────────────────────────────────────────────────

resource "aws_instance" "bastion" {
  ami                         = data.aws_ami.amazon_linux_2.id
  instance_type               = "t2.micro"
  subnet_id                   = aws_subnet.public_a.id
  associate_public_ip_address = true
  iam_instance_profile        = aws_iam_instance_profile.bastion.name
  vpc_security_group_ids      = [aws_security_group.bastion.id]

  root_block_device {
    volume_type           = "gp2"
    volume_size           = 8
    delete_on_termination = true
  }

  tags = merge(var.tags, {
    Name = "${local.prefix}-bastion"
  })
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "bastion_instance_id" {
  description = "Bastion EC2 instance ID - use for SSM port forwarding"
  value       = aws_instance.bastion.id
}

output "bastion_connect_cmd" {
  description = "SSM port-forward command to reach RDS Proxy on localhost:5432"
  value       = <<-EOT
    aws ssm start-session \
      --profile nyk-tf \
      --target ${aws_instance.bastion.id} \
      --document-name AWS-StartPortForwardingSessionToRemoteHost \
      --parameters '{"host":["${aws_db_proxy.main.endpoint}"],"portNumber":["5432"],"localPortNumber":["5432"]}'
  EOT
}
