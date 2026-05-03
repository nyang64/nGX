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

## Issue 9 — `terraform destroy` fails with missing required variable `mail_domain`

**Phase:** teardown (terraform destroy)
**Severity:** High — destroy halts prompting for variable input
**Status:** Fixed in `destroy-env.sh`

### Symptom
```
Error: No value for required variable
  on variables.tf line 61:
  61: variable "mail_domain" {
The root module input variable "mail_domain" is not set, and has no default value.
```

### Root cause
`destroy-env.sh` called `terraform destroy` with only the four script-controlled
vars (`aws_profile`, `aws_region`, `app_name`, `environment`). Required variables
with no defaults (like `mail_domain`, `webhook_encryption_key`) were not set,
causing terraform to prompt interactively.

### Fix applied
`destroy-env.sh` now sources `TF_VAR_*` entries from `.env` before calling
`terraform destroy`, with a save/restore guard around the four script-controlled
vars (same pattern as `setup-env.sh`) to prevent `ENVIRONMENT=production` override.

---

## Issue 10 — `terraform destroy` hangs on VPC deletion due to Lambda ENIs

**Phase:** teardown (terraform destroy, VPC step)
**Severity:** Medium — destroy eventually times out or takes 40+ minutes
**Status:** Handled by `destroy-env.sh` (ENI cleanup added)

### Symptom
Terraform destroy completes everything except the VPC, subnets, and security
groups, then hangs indefinitely with no error output. Remaining resources:
- 2 private subnets
- 1 security group (`ngx-prod-lambda`)
- VPC itself

### Root cause
VPC-attached Lambda functions leave behind Elastic Network Interfaces (ENIs)
tagged `AWS Lambda VPC ENI-<function-name>`. After the Lambda functions are
deleted, AWS asynchronously reclaims these ENIs — but this can take 15–40 minutes.
Terraform waits for the VPC to be deletable, which it cannot be until all ENIs
are released.

### Fix applied
`destroy-env.sh` detects and force-deletes `available` Lambda ENIs in the VPC
before running `terraform destroy`:

```bash
# Delete lingering Lambda VPC ENIs that block subnet/VPC deletion
lambda_enis=$(aws ec2 describe-network-interfaces \
  --filters "Name=vpc-id,Values=${VPC_ID}" \
             "Name=status,Values=available" \
  --query 'NetworkInterfaces[?starts_with(Description,`AWS Lambda VPC ENI`)].NetworkInterfaceId' \
  --output text)
for eni in $lambda_enis; do
  aws ec2 delete-network-interface --network-interface-id "$eni"
done
```

### Notes
- Only ENIs in `available` status are deleted — in-use ENIs are left alone.
- This runs after Lambda functions are deleted (by terraform destroy) but before
  the VPC deletion step. Because terraform deletes resources in dependency order,
  the VPC is always last.
- `destroy-env.sh` adds this cleanup in a post-destroy hook via `-target` ordering.

---

---

## Issue 11 — `list-secrets` misses pending-deletion secrets without `--include-planned-deletion`

**Phase:** setup (pre-apply secret cleanup)
**Severity:** High — terraform apply fails with "secret scheduled for deletion" even after cleanup ran
**Status:** Fixed in `setup-env.sh`

### Symptom
`setup-env.sh` reports "No pending secrets found" and proceeds. Terraform apply then fails:
```
InvalidRequestException: You can't create this secret because a secret with this name
is already scheduled for deletion.
```

### Root cause
AWS `list-secrets` does not return secrets in the pending-deletion state unless
`--include-planned-deletion` is passed. The cleanup loop ran but found nothing,
so the pending-deletion secret was not force-deleted before terraform apply ran.

### Fix applied
Added `--include-planned-deletion` to the `list-secrets` call in setup-env.sh:
```bash
aws_cmd secretsmanager list-secrets \
  --include-planned-deletion \
  --filters "Key=name,Values=${secret_name}" \
  --query 'SecretList[?DeletedDate!=null].ARN' ...
```

---

## Issue 12 — Bastion → RDS Proxy SG egress rule: persistent state drift

**Phase:** setup (Step 5 — SSM tunnel to RDS proxy)
**Severity:** Critical — psql hangs, migrations and bootstrap cannot run
**Status:** Fixed in `setup-env.sh` (post-apply verification step)

### Symptom
After terraform apply completes, the SSM tunnel opens on port 15432 (TCP handshake
succeeds) but psql hangs or times out. Root cause: the bastion security group has
only port 443 egress — port 5432 is missing.

### Root cause
`aws_security_group_rule.bastion_to_rds_proxy` is a standalone SG rule resource
(separate from the SG itself). When terraform applies are interrupted or run in
multiple phases with `-target`, this resource enters a state-drift loop:
- State records a `sgr-*` ID that no longer exists in AWS
- On subsequent applies, terraform detects the drift and tries to recreate the rule
- Due to race conditions or apply interruption, the rule ends up not existing in AWS
  even though state says it does

This happens reliably in the following sequence:
1. First apply interrupted (secret issue, stuck process, etc.)
2. Second apply skips the SG rule because state says it exists
3. SG rule is actually absent from AWS

### Fix applied
`setup-env.sh` now runs a post-apply verification step that checks AWS directly
(not via terraform state) and adds the egress rule if missing:
```bash
existing_port=$(aws ec2 describe-security-groups \
  --group-ids "$BASTION_SG_ID" \
  --query 'SecurityGroups[0].IpPermissionsEgress[?FromPort==`5432`].FromPort' \
  --output text)
if [[ "$existing_port" != "5432" ]]; then
  aws ec2 authorize-security-group-egress \
    --group-id "$BASTION_SG_ID" \
    --ip-permissions "IpProtocol=tcp,FromPort=5432,ToPort=5432,..."
fi
```

This runs unconditionally after every terraform apply, so state drift on this rule
can never block migrations.

### Notes
- The underlying terraform state drift is not fixed — the resource will continue to
  drift. The workaround bypasses terraform for this one rule.
- Long-term fix: move the rule into an inline `egress {}` block on `aws_security_group.bastion`
  so it is part of the SG resource and cannot drift independently.

---

## Issue 13 — `terraform apply` terminates before all resources created; partial state requires manual imports

**Phase:** setup (terraform apply)
**Severity:** High — environment unusable until imports reconcile state
**Status:** Operational workaround documented; fix in progress

### Symptom
After `terraform apply` exits (crash, interrupt, or secret error), the terraform
state is partially written. Subsequent applies fail with:
```
DBInstanceAlreadyExists: DB instance already exists
ResourceConflictException: Function already exist: ngx-prod-search
```

### Root cause
When the first apply fails partway, some resources were created in AWS but their
state was not saved (or saved with wrong IDs). Re-running apply tries to CREATE
them again instead of adopting the existing ones.

### Manual recovery steps
For each resource that already exists in AWS but not state, import it:
```bash
# Aurora instance (count=1, so use [0])
terraform import 'aws_rds_cluster_instance.main[0]' "ngx-prod-instance-0"

# Lambda function
terraform import aws_lambda_function.search "ngx-prod-search"

# SG rule (format: sgId_direction_proto_fromPort_toPort_sourceGroupId)
terraform import aws_security_group_rule.bastion_to_rds_proxy \
  "sg-xxx_egress_tcp_5432_5432_sg-yyy"
```
Then re-run `terraform apply` until it reports `0 to add, N to change, 0 to destroy`.

### Prevention
setup-env.sh now forces a clean state before apply (pre-apply secret cleanup),
which reduces the chance of mid-apply failure. The SG rule drift is handled by the
post-apply verification step (Issue 12 fix).

---

## Issue 14 — RDS Proxy target group has no targets after fresh apply

**Phase:** setup (Step 5 — tunnel to proxy)
**Severity:** High — proxy is available but connections close immediately
**Status:** Operational; `aws_db_proxy_target.main` must be present in state

### Symptom
After terraform apply, the RDS proxy is `available` but psql gets:
```
server closed the connection unexpectedly
```
Checking proxy targets returns `[]` (empty list).

### Root cause
`aws_db_proxy_target.main` was not in state after a partial apply. The proxy
exists but has no registered Aurora cluster as a backend, so it accepts TCP
connections and then immediately closes them.

### Fix applied
After running any partial or targeted apply, ensure the proxy target is created:
```bash
terraform apply -target=aws_db_proxy_target.main
```
Wait ~3 minutes for `TargetHealth.State` to become `AVAILABLE` before connecting.

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
