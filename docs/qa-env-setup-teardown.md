# QA: Environment Setup & Teardown — Known Issues

This document records problems encountered during `scripts/setup-env.sh` and
`scripts/destroy-env.sh` runs, their root causes, and how each was resolved.
Update this file whenever a new issue is found.

---

## Issue 1 — Wrong resource names: `ngx-production-*` instead of `ngx-prod-*`

**Phase:** setup (terraform apply)
**Severity:** Critical — all resources misnamed, entire apply must be torn down
**Status:** Fixed in `setup-env.sh`

### Symptom
All AWS resources created with `ngx-production-*` prefix instead of `ngx-prod-*`.

### Root cause
`.env` contains `ENVIRONMENT=production` (the application runtime value). When
`setup-env.sh` sourced `.env` to pick up `TF_VAR_*` variables, it also overwrote
the script's `ENVIRONMENT=prod` variable (used as the terraform `environment` input).
The script then passed `-var "environment=production"` to terraform.

### Fix applied
Save and restore the four CLI-controlled variables around the `.env` source:

```bash
_SAVE_ENVIRONMENT="$ENVIRONMENT"
# ... source .env ...
ENVIRONMENT="$_SAVE_ENVIRONMENT"
```

### Manual recovery (if it happens again)
1. Run `scripts/destroy-env.sh` to tear down the misnamed environment.
2. If Aurora deletion protection blocks destroy, see **Issue 2**.
3. Re-run `scripts/setup-env.sh` — the variable fix prevents recurrence.

---

## Issue 2 — `terraform destroy` blocked by Aurora deletion protection

**Phase:** teardown
**Severity:** Blocker — destroy hangs waiting for cluster deletion
**Status:** Handled in `destroy-env.sh` (automatic disable + wait)

### Symptom
`terraform destroy` fails with:
```
Error: error deleting RDS Cluster: InvalidParameterCombination:
Cannot delete protected Cluster
```

### Root cause
`aws_rds_cluster.main` has `deletion_protection = true` (intentional for production
safety). Terraform cannot override this inline during destroy.

### Fix applied
`destroy-env.sh` disables deletion protection before running `terraform destroy`:

```bash
aws rds modify-db-cluster \
  --db-cluster-identifier "${PREFIX}-cluster" \
  --no-deletion-protection
# wait ~40s for modification to apply
```

### Notes
- The cluster modification takes ~30–60 seconds before destroy can proceed.
- `destroy-env.sh` polls `DescribeDBClusters` until `DeletionProtection=false`.

---

## Issue 3 — Secrets Manager pending-deletion conflict

**Phase:** setup (terraform apply)
**Severity:** High — apply fails on `aws_secretsmanager_secret` creation
**Status:** Fixed in `setup-env.sh`

### Symptom
```
Error: error creating Secrets Manager Secret: InvalidRequestException:
You can't create this secret because a secret with this name is already
scheduled for deletion.
```

### Root cause
A prior failed or destroyed environment left `ngx-prod/db-password` in the
Secrets Manager "pending deletion" state (default 7-day recovery window). AWS
prevents creating a new secret with the same name until the window expires.

### Fix applied
`setup-env.sh` force-deletes any pending-deletion secret before `terraform apply`:

```bash
pending_arn=$(aws secretsmanager list-secrets \
  --filters "Key=name,Values=${PREFIX}/db-password" \
  --query 'SecretList[?DeletedDate!=null].ARN' \
  --output text)
if [[ -n "$pending_arn" && "$pending_arn" != "None" ]]; then
  aws secretsmanager delete-secret \
    --secret-id "$pending_arn" \
    --force-delete-without-recovery
  sleep 5
fi
```

### Notes
- `--force-delete-without-recovery` is irreversible. Only safe here because the
  secret from the destroyed environment is intentionally being replaced.
- The `sleep 5` gives the API time to propagate the deletion before apply begins.

---

## Issue 4 — Duplicate Lambda permission: `AllowEventBridgeInvokeSchedulerDrafts`

**Phase:** setup (terraform apply)
**Severity:** High — apply fails with `ResourceConflictException`
**Status:** Fixed in `terraform/lambda_triggers.tf`

### Symptom
```
Error: error adding Lambda Permission: ResourceConflictException:
The statement id (AllowEventBridgeInvokeSchedulerDrafts) provided already exists.
```

### Root cause
Two Terraform resources defined the same Lambda permission statement ID:
- `lambda_triggers.tf`: `aws_lambda_permission.scheduler_drafts_eventbridge`
- `eventbridge.tf`: resource inside `aws_lambda_permission` for the same function

### Fix applied
Removed the duplicate resource from `lambda_triggers.tf`. The permission is now
defined only in `eventbridge.tf` alongside the EventBridge rule and target.

### Manual recovery (if state is out of sync)
```bash
# Remove the duplicate from AWS
aws lambda remove-permission \
  --function-name ngx-prod-scheduler-drafts \
  --statement-id AllowEventBridgeInvokeSchedulerDrafts

# Remove one copy from terraform state
terraform state rm aws_lambda_permission.scheduler_drafts_eventbridge
```

---

## Issue 5 — SES configuration set: empty `tracking_options {}` block

**Phase:** setup (terraform apply)
**Severity:** High — apply fails with AWS API error
**Status:** Fixed in `terraform/ses.tf`

### Symptom
```
Error: error updating SES Configuration Set tracking options:
UpdateConfigurationSetTrackingOptions: Missing required field TrackingOptions
```

### Root cause
`aws_ses_configuration_set.main` had an empty `tracking_options {}` block. The
Terraform AWS provider sends an `UpdateConfigurationSetTrackingOptions` API call
with no body when this block is present but empty, which AWS rejects.

### Fix applied
Removed the empty `tracking_options {}` block entirely from `ses.tf`.

---

## Issue 6 — SES configuration set already exists after state removal

**Phase:** setup (terraform apply, during debugging)
**Severity:** Medium — apply fails on re-create after state removal
**Status:** One-time recovery; no code change needed

### Symptom
After removing `aws_ses_configuration_set.main` from terraform state and
manually deleting it in AWS, a subsequent apply failed because AWS still
returned the resource as existing.

### Root cause
AWS SES configuration set deletions have eventual-consistency propagation delay.
The resource appeared deleted in the console but the create API still rejected it.

### Manual recovery
Import the existing resource back into terraform state instead of creating:

```bash
terraform import aws_ses_configuration_set.main ngx-prod-sending
```

---

## Issue 7 — SSM tunnel hangs: bastion cannot reach RDS Proxy on port 5432

**Phase:** setup (DB migrations, Step 5)
**Severity:** Critical — migrations and bootstrap cannot run
**Status:** Fixed by running full `terraform apply` (not targeted)

### Symptom
The SSM port-forward tunnel opens successfully on `localhost:15432` (TCP handshake
with SSM completes), but `psql` hangs indefinitely — the PostgreSQL protocol
handshake never starts.

Direct TCP test from inside the bastion also hangs:
```bash
# via SSM RunCommand — returns errno 11 (EAGAIN/timeout)
python3 -c "import socket; s=socket.socket(); s.connect(('ngx-prod-proxy...', 5432))"
```

### Root cause
State drift: `aws_security_group_rule.bastion_to_rds_proxy` was recorded in
terraform state but the actual egress rule was missing from the bastion security
group in AWS. The bastion SG only had egress port 443 (HTTPS for SSM), so it
could not make outbound TCP connections to port 5432.

The drift was caused by multiple `terraform apply -target=<resource>` runs during
debugging. Targeted applies do not refresh non-targeted resources, leaving the
SG rule as a phantom in state.

### Fix applied
```bash
terraform apply \
  -target=aws_security_group_rule.bastion_to_rds_proxy \
  -var "..." ...
```

Terraform detected the rule was absent and recreated it.

### Prevention
`setup-env.sh` runs a **full** (non-targeted) `terraform apply`. A full apply
always reconciles all resources, so state drift of this kind cannot persist
past the next clean setup run.

### Debugging checklist if tunnel hangs again
1. Check bastion SG egress: must include port 5432 → `sg-<rds_proxy_sg_id>`
2. Check RDS proxy SG ingress: must include port 5432 from `sg-<bastion_sg_id>`
3. Check proxy status: `aws rds describe-db-proxies` → `Status: available`
4. Check proxy target health: `aws rds describe-db-proxy-targets` → `AVAILABLE`
5. Run `terraform apply` (full, no `-target`) to fix any state drift
6. Verify NACLs on proxy subnets allow all traffic (rule 100 allow 0.0.0.0/0)

---

## Issue 8 — `RDS_PROXY_ENDPOINT` missing from `.env.outputs`

**Phase:** setup (Step 5 — DB migrations)
**Severity:** Medium — setup-env.sh falls back to terraform output, but local scripts that rely on `.env.outputs` cannot reach the DB
**Status:** Fixed in `scripts/sync-env.sh`

### Symptom
After running `sync-env.sh`, `.env.outputs` did not contain `RDS_PROXY_ENDPOINT`
or `DB_SECRET_ARN`. Scripts that sourced `.env.outputs` had empty values.

### Fix applied
Added both variables to the `cat > "$OUT_FILE"` block in `sync-env.sh`:

```bash
RDS_PROXY_ENDPOINT=${RDS_PROXY_ENDPOINT}
DB_SECRET_ARN=${DB_SECRET_ARN}
```

---

## Checklist — Before Running `setup-env.sh`

- [ ] `AWS_PROFILE` / `--profile` set to `nyk-tf`
- [ ] `.env` contains all required `TF_VAR_*` variables (see `setup-env.sh` preflight)
- [ ] No pending-deletion secret `<prefix>/db-password` in Secrets Manager
      (setup-env.sh handles this automatically, but verify if a prior run failed mid-way)
- [ ] Terraform state is clean (no leftover resources from a prior partial apply)
- [ ] Bastion instance is running and SSM-registered before Step 5

## Checklist — Before Running `destroy-env.sh`

- [ ] Confirm the correct `--env` / `--app` prefix to avoid deleting the wrong environment
- [ ] Aurora deletion protection will be disabled automatically — this is irreversible for that cluster
- [ ] Final snapshot will be created unless `skip_final_snapshot = true` is set
- [ ] S3 buckets may need manual emptying before destroy if `force_destroy = false`
