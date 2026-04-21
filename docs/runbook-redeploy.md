# Runbook: Full Redeploy (Destroy + Apply)

This runbook covers a complete infrastructure teardown and rebuild using
`terraform destroy` followed by `terraform apply`. Follow every section in
order — skipping steps will cause failures mid-way.

**Estimated time:** 45–90 minutes (most of it waiting on DNS propagation and
Aurora provisioning).

---

## What Recreates Cleanly (No Manual Intervention)

The following resources are fully reproducible by Terraform with no external
dependencies. If you only need to understand what is *risky*, skip to the
next section.

| Resource | Notes |
|----------|-------|
| VPC, subnets, route tables, NAT gateways | Fully declarative |
| Security groups | No external references |
| IAM roles and policies | Fully declarative |
| SQS queues (all 6) | Names are deterministic from locals |
| API Gateway REST API (routes, authorizer, stages) | URL changes — expected |
| API Gateway WebSocket API | URL changes — expected |
| EventBridge scheduled rules | Fully declarative |
| SNS topics and SES→SNS subscriptions | Fully declarative |
| Aurora Serverless v2 cluster + instances | Data is lost — expected on destroy |
| RDS Proxy | Endpoint changes — expected |
| Secrets Manager secret (DB password) | Password regenerated — expected |
| S3 buckets (emails, attachments, artifacts) | Recreated empty |
| Lambda function definitions | Code re-uploaded from local ZIPs |
| Lambda event source mappings (SQS triggers) | Recreated with new queue ARNs |
| CloudWatch log groups | Recreated empty (historical logs lost) |
| Bastion EC2 instance | Recreated; instance ID changes |
| SSM parameters | Fully declarative |

---

## Hard Blockers

These will cause `terraform destroy` or `terraform apply` to **fail outright**
if not addressed first.

### Blocker 1 — Aurora deletion protection

`aurora.tf` sets `deletion_protection = true` (intentional safety guard).
Terraform cannot delete the cluster while this is active.

**Fix — run before `terraform destroy`:**
```bash
aws rds modify-db-cluster \
  --profile nyk-tf --region us-east-1 \
  --db-cluster-identifier ngx-prod-cluster \
  --no-deletion-protection
```

### Blocker 2 — S3 buckets not empty

All three buckets have `force_destroy = false`. Terraform will refuse to delete
a bucket that contains objects.

**Fix — run before `terraform destroy`:**
```bash
# Emails and attachments — destructive, back up first if needed
aws s3 rm s3://ngx-prod-emails       --recursive --profile nyk-tf --region us-east-1
aws s3 rm s3://ngx-prod-attachments  --recursive --profile nyk-tf --region us-east-1
aws s3 rm s3://ngx-prod-lambda-artifacts --recursive --profile nyk-tf --region us-east-1
```

> **Data warning:** Emails and attachments stored in S3 are permanently deleted.
> Export anything you need to keep before running these commands.

### Blocker 3 — Lambda ZIPs must exist before `terraform plan`

Every Lambda resource uses `filebase64sha256("../dist/lambdas/<name>.zip")`.
Terraform reads these files at **plan time** — if any ZIP is missing, the plan
fails before a single resource is touched.

**Fix — run before `terraform apply`:**
```bash
make build-lambdas
```

---

## Full Redeploy Procedure

### Step 1 — Pre-destroy preparation

```bash
# Disable Aurora deletion protection
aws rds modify-db-cluster \
  --profile nyk-tf --region us-east-1 \
  --db-cluster-identifier ngx-prod-cluster \
  --no-deletion-protection

# Empty S3 buckets (back up data first if needed)
aws s3 rm s3://ngx-prod-emails           --recursive --profile nyk-tf --region us-east-1
aws s3 rm s3://ngx-prod-attachments      --recursive --profile nyk-tf --region us-east-1
aws s3 rm s3://ngx-prod-lambda-artifacts --recursive --profile nyk-tf --region us-east-1
```

### Step 2 — Destroy

```bash
source loadenv.sh
AWS_PROFILE=nyk-tf terraform -chdir=terraform destroy
```

Expect this to take 15–25 minutes. Aurora and NAT gateways are the slowest to
delete.

### Step 3 — Build Lambda ZIPs

```bash
make build-lambdas
# Outputs: dist/lambdas/<name>.zip for all 19 Lambda functions
```

### Step 4 — Apply

```bash
source loadenv.sh
AWS_PROFILE=nyk-tf terraform -chdir=terraform apply
```

Expect 20–30 minutes. Aurora cluster provisioning and RDS Proxy creation
dominate the wait.

### Step 5 — Regenerate post-deploy environment

All resource IDs change on a fresh apply (API GW IDs, SQS URLs, RDS proxy
endpoint). Regenerate `.env.outputs`:

```bash
scripts/sync-env.sh --profile nyk-tf
source loadenv.sh
```

Verify the key values look reasonable:
```bash
echo $REST_API_ENDPOINT
echo $DATABASE_URL
echo $SQS_EMAIL_INBOUND_URL
```

### Step 6 — Add SES DNS records

Terraform creates the SES domain identity and DKIM tokens but **cannot write
DNS records** — that is always a manual step with your DNS provider.

Get the records to add:
```bash
# Domain verification TXT record
terraform -chdir=terraform output ses_verification_token

# DKIM CNAME records (3 records)
terraform -chdir=terraform output ses_dkim_tokens
```

Add to DNS:
| Type | Host | Value |
|------|------|-------|
| TXT | `_amazonses.mail.agent-mx.cc` | Verification token from output |
| CNAME | `<token1>._domainkey.mail.agent-mx.cc` | `<token1>.dkim.amazonses.com` |
| CNAME | `<token2>._domainkey.mail.agent-mx.cc` | `<token2>.dkim.amazonses.com` |
| CNAME | `<token3>._domainkey.mail.agent-mx.cc` | `<token3>.dkim.amazonses.com` |

Then wait for DNS propagation and check status:
```bash
# Poll until Status = "Success" (usually 5–15 minutes)
aws ses get-identity-verification-attributes \
  --profile nyk-tf --region us-east-1 \
  --identities mail.agent-mx.cc \
  --query 'VerificationAttributes.*.VerificationStatus'
```

### Step 7 — Run database migrations

The fresh Aurora cluster has no schema. Connect via SSM tunnel and migrate:

```bash
# Open SSM tunnel (leaves it running in background)
aws ssm start-session \
  --profile nyk-tf --region us-east-1 \
  --target i-<new-bastion-instance-id> \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters '{"host":["'"$RDS_PROXY_ENDPOINT"'"],"portNumber":["5432"],"localPortNumber":["15432"]}' &

# Run migrations
DATABASE_URL="postgres://${TF_VAR_db_username}:<password>@127.0.0.1:15432/${TF_VAR_db_name}?sslmode=verify-ca&sslrootcert=/usr/local/etc/openssl@3/cert.pem" \
  make migrate-up
```

> Get `<password>` from Secrets Manager:
> ```bash
> aws secretsmanager get-secret-value \
>   --profile nyk-tf --region us-east-1 \
>   --secret-id $(terraform -chdir=terraform output -raw db_secret_arn) \
>   --query SecretString --output text | jq -r '.password'
> ```

### Step 8 — Deploy Lambda code

A fresh apply creates Lambda functions but with environment variables pointing
to the old (now-destroyed) resources. Redeploy to push the new URLs and the
new database password:

```bash
make deploy-lambdas
```

This uploads all ZIPs to S3 and calls `aws lambda update-function-code` for
each function. Lambda picks up the new environment variables set by Terraform
on the next invocation.

### Step 9 — Bootstrap initial org (first-time only)

If the database is brand new (no data migrated), create the seed org and API
key:

```bash
make bootstrap org="nyklabs" slug="nyklabs"
# Prints: am_live_xxxx  — save this immediately
```

### Step 10 — Smoke test

```bash
source loadenv.sh

# REST API health
curl -s ${REST_API_ENDPOINT}/v1/org \
  -H "Authorization: Bearer ${API_KEY}" | jq .

# Send a test email
curl -s -X POST ${REST_API_ENDPOINT}/v1/inboxes/<inbox-id>/messages/send \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"to":[{"email":"success@simulator.amazonses.com"}],"subject":"smoke test","body_text":"ok"}'
```

---

## What Changes After a Fresh Apply

These values are **not stable across deploys** — always re-run
`scripts/sync-env.sh` after apply and re-run `make deploy-lambdas` so Lambdas
pick up the new values.

| Value | Why it changes |
|-------|---------------|
| REST API Gateway URL | New `aws_api_gateway_rest_api` resource gets a new ID |
| WebSocket API URL | New `aws_apigatewayv2_api` resource gets a new ID |
| RDS Proxy endpoint | New proxy resource, new hostname |
| All SQS queue URLs | Account/region-stable names, but full URLs include the account ID path |
| Aurora DB password | `random_password` resource regenerates on every fresh apply |
| Bastion instance ID | New EC2 instance |

---

## Partial Redeploy (Lambdas Only)

If you only need to redeploy Lambda code without touching infrastructure:

```bash
make build-lambdas
make deploy-lambdas
```

This is safe to run at any time and takes ~3 minutes.

---

## Partial Redeploy (Single Lambda)

```bash
make deploy-lambda-<name>
# e.g.
make deploy-lambda-inboxes
make deploy-lambda-email_inbound
```

---

## Terraform State

State is stored locally at `terraform/terraform.tfstate`. If you lose this
file, Terraform cannot track existing resources and a fresh apply will attempt
to create duplicates (which will fail on name conflicts).

**Recommendation:** Configure a remote backend (S3 + DynamoDB) before relying
on this stack in production:

```hcl
# terraform/main.tf — add this block
terraform {
  backend "s3" {
    bucket         = "ngx-tf-state"
    key            = "prod/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "ngx-tf-state-lock"
    encrypt        = true
  }
}
```
