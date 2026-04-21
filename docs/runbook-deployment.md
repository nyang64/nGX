# nGX Deployment Runbook

This document covers two scenarios:

- **[Part A — Fresh Deployment](#part-a--fresh-deployment):** Standing up nGX
  for the first time in a new AWS account.
- **[Part B — Redeployment](#part-b--redeployment):** Tearing down an existing
  stack and rebuilding it from scratch.

Both share the same [prerequisites](#prerequisites) and the same
[terraform paver profile](#the-terraform-paver-profile) concept. Read those
sections first regardless of which scenario applies.

---

## Prerequisites

All of the following must be in place **before you run a single Terraform
command**. Skipping any item will cause the deployment to fail mid-way or
produce a stack that cannot receive or send email.

### 1. AWS account

You need an AWS account in which you have permission to provision all required
services. The account must have the following service limits not at zero (all
are available by default in a new account):

- Lambda, API Gateway, Aurora Serverless v2, RDS Proxy
- SES (see SES-specific requirements below)
- VPC (default limit is 5 VPCs per region — this stack creates 1)
- EC2 t2.micro (bastion)
- S3, SQS, SNS, Secrets Manager, CloudWatch, EventBridge, SSM

nGX deploys into a single AWS region. Choose the region before you start
and do not change it later without a full destroy + redeploy.

### 2. Terraform paver role

You need an AWS IAM principal (user or role) with broad infrastructure
permissions. See [The Terraform Paver Profile](#the-terraform-paver-profile)
for the full setup guide. The short version:

- Attach `AdministratorAccess` (or a scoped equivalent) to a dedicated
  IAM user or role.
- Configure it as a named AWS CLI profile (e.g. `my-tf-paver`).
- Never use this principal for application-level operations.

### 3. Mail domain

nGX needs a subdomain that it owns exclusively for email. All inboxes are
provisioned under this domain (e.g. `agent@mail.yourdomain.com`).

**Requirements:**

- You own the apex domain (e.g. `yourdomain.com`) or have DNS control over
  a subdomain.
- Use a **subdomain** as the mail domain (e.g. `mail.yourdomain.com`) rather
  than the apex, so email DNS records do not interfere with your web presence.
- The domain must be publicly resolvable — it must be registered in a public
  DNS provider or AWS Route 53.

### 4. SES production access (sandbox lifted)

New AWS accounts start with SES in **sandbox mode**, which restricts sending
to verified addresses only and caps daily volume. nGX requires SES production
access so it can send to arbitrary recipients.

**How to request production access:**

1. Open the AWS SES console → *Account dashboard* → *Request production access*.
2. Fill in the use case form. Key fields:
   - **Mail type:** Transactional
   - **Website URL:** your product or company URL
   - **Use case description:** explain that you are running a self-hosted email
     platform for AI agent communication and will only send transactional mail
     initiated by your users.
3. Submit. AWS typically approves within 24–48 hours.

> You can complete the rest of the deployment while waiting for SES approval.
> The stack will apply successfully; inbound works immediately; outbound to
> external addresses will fail until sandbox is lifted.

### 5. DNS records for the mail domain

Several DNS records must exist **before email can flow**. Some records can
only be obtained after `terraform apply` (the DKIM CNAMEs and verification
TXT), but the MX and SPF records can be added in advance.

| Record | Can be added | Notes |
|--------|-------------|-------|
| MX | Before apply | Points the domain at SES inbound SMTP |
| SPF TXT | Before apply | Authorises SES to send on your behalf |
| DMARC TXT | Before apply | Sets bounce/complaint policy |
| SES verification TXT | After apply | Token provided by `terraform output` |
| DKIM CNAMEs (×3) | After apply | Tokens provided by `terraform output` |

**MX record** (SES inbound endpoint varies by region):

| Region | MX value |
|--------|----------|
| us-east-1 | `10 inbound-smtp.us-east-1.amazonaws.com` |
| us-west-2 | `10 inbound-smtp.us-west-2.amazonaws.com` |
| eu-west-1 | `10 inbound-smtp.eu-west-1.amazonaws.com` |

Add this to your mail subdomain:
```
mail.yourdomain.com.  MX  10 inbound-smtp.<region>.amazonaws.com
```

**SPF TXT record:**
```
mail.yourdomain.com.  TXT  "v=spf1 include:amazonses.com ~all"
```

**DMARC TXT record** (adjust `rua` to a real reporting address):
```
_dmarc.mail.yourdomain.com.  TXT  "v=DMARC1; p=quarantine; rua=mailto:dmarc@yourdomain.com"
```

### 6. Local toolchain

| Tool | Minimum version | Purpose |
|------|----------------|---------|
| Go | 1.23 | Build Lambda binaries |
| Terraform | 1.5 | Provision infrastructure |
| AWS CLI | 2.x | All AWS interactions |
| `jq` | any | Parse JSON in shell scripts |
| `make` | any | Build and deploy targets |
| `openssl` | any | Generate encryption keys |

Install the AWS CLI session manager plugin (required for SSM tunnels):
```bash
# macOS
brew install --cask session-manager-plugin

# Linux — see https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html
```

---

## The Terraform Paver Profile

All commands in this runbook that touch AWS infrastructure require an AWS CLI
profile with broad IAM permissions — enough to create, modify, and destroy
VPCs, IAM roles, Lambda functions, Aurora clusters, SQS queues, S3 buckets,
API Gateway, SES identities, and more. We call this a **terraform paver
profile**: a dedicated AWS profile whose sole purpose is to provision and
tear down infrastructure.

In nyklabs' own deployment the profile is named **`nyk-tf`**. If you are
deploying nGX into your own AWS account you will need an equivalent profile
under a name of your choosing.

### What the paver profile needs

The paver profile must be configured in `~/.aws/config` and point to an IAM
principal (user or assumed role) with at minimum:

- Full permissions on: IAM, VPC/EC2, Lambda, API Gateway, SES, SQS, SNS,
  S3, RDS/Aurora, RDS Proxy, Secrets Manager, CloudWatch Logs, EventBridge,
  SSM, and ACM
- The ability to pass IAM roles to Lambda (`iam:PassRole`)

A simple starting point is attaching `AdministratorAccess` to a dedicated
infrastructure IAM user or role and never using it for anything other than
Terraform runs. Scope it down after the initial deployment if your security
posture requires it.

### Configuring your paver profile

```ini
# ~/.aws/config
[profile my-tf-paver]
region = <your-aws-region>
```

```ini
# ~/.aws/credentials
[my-tf-paver]
aws_access_key_id     = AKIA...
aws_secret_access_key = ...
```

Or use an assumed role:

```ini
# ~/.aws/config
[profile my-tf-paver]
role_arn       = arn:aws:iam::<account-id>:role/TerraformPaverRole
source_profile = default
region         = <your-aws-region>
```

### Setting your profile name and region

Export both variables once at the start of your session. Every command in
this runbook uses them so you never need to edit individual steps:

```bash
export TF_PAVER_PROFILE=my-tf-paver   # your paver profile name
export TF_AWS_REGION=us-east-1        # the AWS region you are deploying into
```

> **Important:** Do not rely on the region configured inside the AWS profile —
> it may differ from the region you are deploying to. Always pass
> `--region $TF_AWS_REGION` explicitly, as shown in each step.

---

## What Recreates Cleanly (No Manual Intervention)

The following resources are fully reproducible by Terraform with no external
dependencies. This applies equally to a fresh deploy and a redeploy.

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
| Aurora Serverless v2 cluster + instances | Data is lost on destroy — expected |
| RDS Proxy | Endpoint changes — expected |
| Secrets Manager secret (DB password) | Password regenerated — expected |
| S3 buckets (emails, attachments, artifacts) | Recreated empty |
| Lambda function definitions | Code re-uploaded from local ZIPs |
| Lambda event source mappings (SQS triggers) | Recreated with new queue ARNs |
| CloudWatch log groups | Recreated empty (historical logs lost) |
| Bastion EC2 instance | Recreated; instance ID changes |
| SSM parameters | Fully declarative |

---

## Part A — Fresh Deployment

Use this section when deploying nGX into an AWS account for the first time.

**Estimated time:** 60–120 minutes (most of it waiting on Aurora provisioning,
SES domain verification, and DNS propagation).

### A1 — Configure environment

Clone the repository and create your `.env` file:

```bash
git clone <repo-url> && cd nGX
cp .env.example .env
```

Edit `.env` and fill in every value. The minimum required set:

```bash
# Terraform inputs
TF_VAR_app_name=ngx
TF_VAR_environment=prod
TF_VAR_aws_region=us-east-1          # must match TF_AWS_REGION
TF_VAR_mail_domain=mail.yourdomain.com
TF_VAR_db_name=ngx
TF_VAR_db_username=ngxadmin
TF_VAR_webhook_encryption_key=$(openssl rand -hex 32)   # generate once, keep safe

# Application runtime
ENVIRONMENT=production
LOG_LEVEL=info
LOG_FORMAT=json
AWS_REGION=us-east-1
MAIL_DOMAIN=mail.yourdomain.com
SES_CONFIGURATION_SET=ngx-prod-sending
SES_RULE_SET_NAME=ngx-prod-receipt-rules
WEBHOOK_ENCRYPTION_KEY=${TF_VAR_webhook_encryption_key}
```

> **Save `TF_VAR_webhook_encryption_key`** somewhere secure (password manager,
> Secrets Manager). It encrypts webhook auth headers in the database. Losing
> it means stored webhook credentials cannot be decrypted.

### A2 — Add pre-apply DNS records

Add the MX, SPF, and DMARC records to your DNS provider now (see
[Prerequisites §5](#5-dns-records-for-the-mail-domain)). These do not depend
on Terraform output and can propagate while infrastructure is being built.

### A3 — Build Lambda ZIPs

```bash
make build-lambdas
# Outputs: dist/lambdas/<name>.zip for all 19 Lambda functions
```

Terraform reads the ZIP files at plan time. If any are missing the plan fails
before a single resource is created.

### A4 — Initialise and apply Terraform

```bash
source loadenv.sh
cd terraform && terraform init && cd ..
AWS_PROFILE=$TF_PAVER_PROFILE AWS_REGION=$TF_AWS_REGION terraform -chdir=terraform apply
```

Expect 20–35 minutes. Aurora cluster provisioning and RDS Proxy creation
dominate the wait. Review the plan carefully before typing `yes`.

After apply, print the post-deploy checklist:

```bash
terraform -chdir=terraform output post_deploy_instructions
```

### A5 — Generate post-deploy environment

```bash
scripts/sync-env.sh --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
source loadenv.sh
```

Verify the key values:

```bash
echo $REST_API_ENDPOINT
echo $DATABASE_URL
echo $SQS_EMAIL_INBOUND_URL
```

### A6 — Add post-apply DNS records (SES verification + DKIM)

Terraform has now created the SES domain identity and generated DKIM signing
keys. Retrieve the DNS records to add:

```bash
# SES domain verification TXT record
terraform -chdir=terraform output ses_verification_token

# DKIM CNAME records (3 records)
terraform -chdir=terraform output ses_dkim_tokens
```

Add to your DNS provider:

| Type | Host | Value |
|------|------|-------|
| TXT | `_amazonses.mail.yourdomain.com` | Verification token from output |
| CNAME | `<token1>._domainkey.mail.yourdomain.com` | `<token1>.dkim.amazonses.com` |
| CNAME | `<token2>._domainkey.mail.yourdomain.com` | `<token2>.dkim.amazonses.com` |
| CNAME | `<token3>._domainkey.mail.yourdomain.com` | `<token3>.dkim.amazonses.com` |

Poll until verified (usually 5–15 minutes):

```bash
aws ses get-identity-verification-attributes \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --identities $TF_VAR_mail_domain \
  --query 'VerificationAttributes.*.VerificationStatus'
# Wait for: ["Success"]
```

### A7 — Run database migrations

The Aurora cluster exists but has no schema. Open an SSM tunnel to forward
the RDS Proxy port to your local machine, then migrate:

```bash
# Get the bastion instance ID from terraform output
BASTION_ID=$(terraform -chdir=terraform output -raw bastion_instance_id 2>/dev/null || \
  aws ec2 describe-instances \
    --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
    --filters "Name=tag:Name,Values=ngx-prod-bastion" "Name=instance-state-name,Values=running" \
    --query 'Reservations[0].Instances[0].InstanceId' --output text)

# Open SSM tunnel (runs in background)
aws ssm start-session \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --target $BASTION_ID \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters "{\"host\":[\"$RDS_PROXY_ENDPOINT\"],\"portNumber\":[\"5432\"],\"localPortNumber\":[\"15432\"]}" &

# Get the DB password from Secrets Manager
DB_PASSWORD=$(aws secretsmanager get-secret-value \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --secret-id $(terraform -chdir=terraform output -raw db_secret_arn) \
  --query SecretString --output text | jq -r '.password')

# Run migrations
DATABASE_URL="postgres://${TF_VAR_db_username}:${DB_PASSWORD}@127.0.0.1:15432/${TF_VAR_db_name}?sslmode=verify-ca&sslrootcert=/usr/local/etc/openssl@3/cert.pem" \
  make migrate-up
```

### A8 — Deploy Lambda code

Terraform created the Lambda function resources but they need the compiled
code uploaded. This step also synchronises Lambda environment variables with
the current Terraform state:

```bash
make deploy-lambdas
```

### A9 — Bootstrap the first org

Create the initial organisation and admin API key:

```bash
source loadenv.sh
make bootstrap org="My Org" slug="my-org"
# Save the printed API key immediately: am_live_xxxx
export API_KEY=am_live_xxxx
```

### A10 — Smoke test

```bash
source loadenv.sh

# Verify the API is reachable
curl -s ${REST_API_ENDPOINT}/v1/org \
  -H "Authorization: Bearer ${API_KEY}" | jq .

# Create a pod and inbox
POD_ID=$(curl -s -X POST ${REST_API_ENDPOINT}/v1/pods \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test","slug":"test"}' | jq -r '.id')

INBOX_ID=$(curl -s -X POST ${REST_API_ENDPOINT}/v1/inboxes \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d "{\"pod_id\":\"$POD_ID\",\"address\":\"agent\"}" | jq -r '.id')

# Send a test email (SES simulator — works even in sandbox)
curl -s -X POST ${REST_API_ENDPOINT}/v1/inboxes/${INBOX_ID}/messages/send \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"to":[{"email":"success@simulator.amazonses.com"}],"subject":"smoke test","body_text":"ok"}' | jq .
```

---

## Part B — Redeployment

Use this section when tearing down an existing nGX stack and rebuilding it.
This is appropriate for disaster recovery, region migration, or major
infrastructure changes.

**Estimated time:** 45–90 minutes.

> **Data warning:** Destroy permanently deletes the Aurora cluster and all S3
> objects. Back up anything you need to keep before proceeding.

### Hard blockers (must resolve before destroy)

#### Blocker 1 — Aurora deletion protection

`aurora.tf` sets `deletion_protection = true` as an intentional safety guard.
Terraform cannot delete the cluster while this is active.

```bash
aws rds modify-db-cluster \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --db-cluster-identifier ngx-prod-cluster \
  --no-deletion-protection
```

#### Blocker 2 — S3 buckets not empty

All three buckets have `force_destroy = false`. Terraform refuses to delete
a bucket that contains objects.

```bash
aws s3 rm s3://ngx-prod-emails           --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
aws s3 rm s3://ngx-prod-attachments      --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
aws s3 rm s3://ngx-prod-lambda-artifacts --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
```

#### Blocker 3 — Lambda ZIPs must exist before plan

Terraform reads `dist/lambdas/<name>.zip` at plan time via
`filebase64sha256()`. If `dist/` was cleaned, rebuild before applying:

```bash
make build-lambdas
```

### B1 — Pre-destroy preparation

```bash
# Disable deletion protection
aws rds modify-db-cluster \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --db-cluster-identifier ngx-prod-cluster \
  --no-deletion-protection

# Empty S3 buckets (back up data first)
aws s3 rm s3://ngx-prod-emails           --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
aws s3 rm s3://ngx-prod-attachments      --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
aws s3 rm s3://ngx-prod-lambda-artifacts --recursive --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
```

### B2 — Destroy

```bash
source loadenv.sh
AWS_PROFILE=$TF_PAVER_PROFILE AWS_REGION=$TF_AWS_REGION terraform -chdir=terraform destroy
```

Expect 15–25 minutes. Aurora and NAT gateways are the slowest to delete.

### B3 — Build Lambda ZIPs

```bash
make build-lambdas
```

### B4 — Apply

```bash
source loadenv.sh
AWS_PROFILE=$TF_PAVER_PROFILE AWS_REGION=$TF_AWS_REGION terraform -chdir=terraform apply
```

Expect 20–30 minutes.

### B5 — Regenerate post-deploy environment

All resource IDs change on a fresh apply. Regenerate `.env.outputs`:

```bash
scripts/sync-env.sh --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION
source loadenv.sh
```

### B6 — Re-add SES DNS records

The SES domain identity is recreated with new DKIM tokens. The old CNAME
records in DNS are now stale and must be replaced.

```bash
terraform -chdir=terraform output ses_verification_token
terraform -chdir=terraform output ses_dkim_tokens
```

Replace the three DKIM CNAME records and the verification TXT record in your
DNS provider, then verify:

```bash
aws ses get-identity-verification-attributes \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --identities $TF_VAR_mail_domain \
  --query 'VerificationAttributes.*.VerificationStatus'
```

### B7 — Run database migrations

```bash
BASTION_ID=$(aws ec2 describe-instances \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --filters "Name=tag:Name,Values=ngx-prod-bastion" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' --output text)

aws ssm start-session \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --target $BASTION_ID \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters "{\"host\":[\"$RDS_PROXY_ENDPOINT\"],\"portNumber\":[\"5432\"],\"localPortNumber\":[\"15432\"]}" &

DB_PASSWORD=$(aws secretsmanager get-secret-value \
  --profile $TF_PAVER_PROFILE --region $TF_AWS_REGION \
  --secret-id $(terraform -chdir=terraform output -raw db_secret_arn) \
  --query SecretString --output text | jq -r '.password')

DATABASE_URL="postgres://${TF_VAR_db_username}:${DB_PASSWORD}@127.0.0.1:15432/${TF_VAR_db_name}?sslmode=verify-ca&sslrootcert=/usr/local/etc/openssl@3/cert.pem" \
  make migrate-up
```

### B8 — Deploy Lambda code

```bash
make deploy-lambdas
```

### B9 — Bootstrap initial org (if starting fresh)

```bash
source loadenv.sh
make bootstrap org="My Org" slug="my-org"
export API_KEY=am_live_xxxx
```

### B10 — Smoke test

Same as [A10](#a10--smoke-test) above.

---

## What Changes After a Fresh Apply

These values are **not stable across deploys**. Always re-run
`scripts/sync-env.sh` after apply and `make deploy-lambdas` so Lambdas pick
up the new values.

| Value | Why it changes |
|-------|---------------|
| REST API Gateway URL | New `aws_api_gateway_rest_api` resource gets a new ID |
| WebSocket API URL | New `aws_apigatewayv2_api` resource gets a new ID |
| RDS Proxy endpoint | New proxy resource, new hostname |
| All SQS queue URLs | Full URLs include the AWS account ID path |
| Aurora DB password | `random_password` resource regenerates on every fresh apply |
| Bastion instance ID | New EC2 instance |

---

## Partial Redeploy (Lambdas Only)

If you only need to push updated Lambda code without touching infrastructure:

```bash
make build-lambdas
make deploy-lambdas
```

Safe to run at any time. Takes ~3 minutes.

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
    region         = "<your-aws-region>"
    dynamodb_table = "ngx-tf-state-lock"
    encrypt        = true
  }
}
```
