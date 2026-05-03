#!/usr/bin/env bash
# setup-env.sh — Stand up a complete nGX environment from scratch.
#
# Order of operations:
#   1. Pre-flight checks (tools, .env, AWS credentials)
#   2. Build Lambda ZIPs
#   3. Terraform init + apply
#   4. Generate post-deploy env (.env.outputs via sync-env.sh)
#   5. Wait for bastion SSM readiness
#   6. Run database migrations via SSM tunnel
#   7. Deploy Lambda code (make deploy-lambdas)
#   8. Bootstrap initial org (unless --skip-bootstrap)
#   9. Print post-apply DNS records to add (SES verification + DKIM)
#  10. Smoke test
#
# Usage:
#   ./scripts/setup-env.sh [--profile <aws-profile>] [--region <region>] \
#                          [--app <app_name>] [--env <environment>] \
#                          [--tf-dir <path>] [--repo-root <path>] \
#                          [--org <org-name>] [--slug <org-slug>] \
#                          [--skip-bootstrap] [--skip-smoke] [--yes]
#
# Defaults match the current prod stack:
#   profile = nyk-tf   region = us-east-1   app = ngx   env = prod
#
# Pass --yes to skip the "are you sure?" confirmation.

set -euo pipefail

# ─── Defaults ────────────────────────────────────────────────────────────────
AWS_PROFILE="${AWS_PROFILE:-nyk-tf}"
AWS_REGION="us-east-1"
APP_NAME="ngx"
ENVIRONMENT="prod"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TF_DIR="${REPO_ROOT}/terraform"
ORG_NAME=""
ORG_SLUG=""
SKIP_BOOTSTRAP=false
SKIP_SMOKE=false
AUTO_APPROVE=false

SSM_TUNNEL_PID=""

# ─── Argument parsing ────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)         AWS_PROFILE="$2";       shift 2 ;;
    --region)          AWS_REGION="$2";        shift 2 ;;
    --app)             APP_NAME="$2";          shift 2 ;;
    --env)             ENVIRONMENT="$2";       shift 2 ;;
    --tf-dir)          TF_DIR="$2";            shift 2 ;;
    --repo-root)       REPO_ROOT="$2";         shift 2 ;;
    --org)             ORG_NAME="$2";          shift 2 ;;
    --slug)            ORG_SLUG="$2";          shift 2 ;;
    --skip-bootstrap)  SKIP_BOOTSTRAP=true;    shift   ;;
    --skip-smoke)      SKIP_SMOKE=true;        shift   ;;
    --yes)             AUTO_APPROVE=true;       shift   ;;
    *) echo "Unknown option: $1"; exit 1       ;;
  esac
done

PREFIX="${APP_NAME}-${ENVIRONMENT}"

# ─── Helpers ─────────────────────────────────────────────────────────────────
log()  { echo "[$(date -u +%H:%M:%S)] $*"; }
warn() { echo "[$(date -u +%H:%M:%S)] WARN: $*" >&2; }
die()  { echo "[$(date -u +%H:%M:%S)] ERROR: $*" >&2; exit 1; }

aws_cmd() {
  aws --profile "$AWS_PROFILE" --region "$AWS_REGION" "$@"
}

# Kill the SSM tunnel on exit so it doesn't linger
cleanup() {
  if [[ -n "$SSM_TUNNEL_PID" ]]; then
    log "Closing SSM tunnel (pid $SSM_TUNNEL_PID)..."
    kill "$SSM_TUNNEL_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

# ─── Pre-flight ───────────────────────────────────────────────────────────────
log "=== nGX Environment Setup Script ==="
log "  Profile   : $AWS_PROFILE"
log "  Region    : $AWS_REGION"
log "  Prefix    : $PREFIX"
log "  TF dir    : $TF_DIR"
log "  Repo root : $REPO_ROOT"
echo

# Required tools
for tool in aws terraform jq go make openssl; do
  command -v "$tool" &>/dev/null || die "Required tool not found: $tool"
done

# Session manager plugin (needed for SSM port-forwarding)
command -v session-manager-plugin &>/dev/null \
  || die "AWS SSM Session Manager plugin not installed. See: https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-working-with-install-plugin.html"

# .env must exist and have key fields filled in
[[ -f "${REPO_ROOT}/.env" ]] \
  || die ".env not found in ${REPO_ROOT}. Copy .env.example → .env and fill in all values."

for required_var in TF_VAR_mail_domain TF_VAR_webhook_encryption_key; do
  value=$(grep -E "^${required_var}=" "${REPO_ROOT}/.env" 2>/dev/null | cut -d= -f2- || true)
  [[ -n "$value" && "$value" != "" ]] \
    || die ".env is missing or has empty value for: ${required_var}"
done

[[ -d "$TF_DIR" ]] || die "Terraform directory not found: $TF_DIR"

# Verify AWS credentials
aws_cmd sts get-caller-identity --query 'Account' --output text > /dev/null \
  || die "AWS credentials not working for profile '$AWS_PROFILE'"

log "Pre-flight OK — AWS account: $(aws_cmd sts get-caller-identity --query 'Account' --output text)"
echo

# ─── Confirmation ─────────────────────────────────────────────────────────────
if [[ "$AUTO_APPROVE" != "true" ]]; then
  echo "This will deploy a complete ${PREFIX} nGX environment:"
  echo "  - VPC, subnets, NAT gateways"
  echo "  - Aurora Serverless v2 cluster + RDS Proxy"
  echo "  - Bastion EC2, SQS queues, S3 buckets"
  echo "  - API Gateway (REST + WebSocket)"
  echo "  - Lambda functions (19 total)"
  echo "  - SES domain identity for $(grep -E '^TF_VAR_mail_domain=' "${REPO_ROOT}/.env" | cut -d= -f2-)"
  echo
  echo "Estimated time: 60–120 minutes (Aurora + SSM agent startup dominate the wait)."
  echo
  read -r -p "Continue? [y/N]: " confirm
  [[ "$confirm" =~ ^[yY]$ ]] || die "Aborted."
fi

echo
log ">>> Starting setup of ${PREFIX} environment <<<"
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 1 — Build Lambda ZIPs
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 1: Build Lambda ZIPs ==="

cd "$REPO_ROOT"
make build-lambdas

# Verify at least one ZIP was produced
ZIP_COUNT=$(find dist/lambdas -name '*.zip' 2>/dev/null | wc -l | tr -d ' ')
[[ "$ZIP_COUNT" -gt 0 ]] || die "No Lambda ZIPs found in dist/lambdas/ after build."
log "Built ${ZIP_COUNT} Lambda ZIPs."

log "STEP 1 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 2 — Terraform init + apply
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 2: Terraform init + apply ==="

# Source .env so TF_VAR_* variables are exported into the environment.
# IMPORTANT: .env contains ENVIRONMENT=production (the app runtime value) which
# would overwrite the script's ENVIRONMENT=prod (the terraform resource suffix).
# Save and restore the script-level CLI arg values after sourcing to prevent that.
_SAVE_APP_NAME="$APP_NAME"
_SAVE_ENVIRONMENT="$ENVIRONMENT"
_SAVE_AWS_REGION="$AWS_REGION"
_SAVE_AWS_PROFILE="$AWS_PROFILE"

set -a
# shellcheck disable=SC1090
source <(grep -v '^#' "${REPO_ROOT}/.env" | grep -v '^$' | sed 's/[[:space:]]*#.*$//')
set +a

# Restore CLI-specified values (override whatever .env set for these keys)
APP_NAME="$_SAVE_APP_NAME"
ENVIRONMENT="$_SAVE_ENVIRONMENT"
AWS_REGION="$_SAVE_AWS_REGION"
AWS_PROFILE="$_SAVE_AWS_PROFILE"
PREFIX="${APP_NAME}-${ENVIRONMENT}"

export AWS_PROFILE AWS_REGION

log "Running terraform init..."
terraform -chdir="$TF_DIR" init

# Pre-apply: force-delete any Secrets Manager secrets that are scheduled for deletion.
# AWS enforces a 7–30 day recovery window which blocks re-creation with the same name.
# This situation occurs when a previous apply created the secret and then failed —
# terraform destroy puts it in pending-deletion but can't immediately remove it.
log "Checking for Secrets Manager secrets pending deletion..."
for secret_name in "${PREFIX}/db-password"; do
  pending_arn=$(aws_cmd secretsmanager list-secrets \
    --include-planned-deletion \
    --filters "Key=name,Values=${secret_name}" \
    --query 'SecretList[?DeletedDate!=null].ARN' \
    --output text 2>/dev/null || true)
  if [[ -n "$pending_arn" && "$pending_arn" != "None" ]]; then
    log "  Force-deleting pending secret: ${secret_name}"
    aws_cmd secretsmanager delete-secret \
      --secret-id "$pending_arn" \
      --force-delete-without-recovery \
      || warn "Failed to force-delete ${secret_name} — terraform apply may fail if it still exists."
    sleep 5   # Allow propagation before apply
  else
    log "  No pending secrets found for: ${secret_name}"
  fi
done

# ── Terraform apply with AlreadyExists auto-import retry ────────────────────
#
# Known drift-prone resources: if AWS already has them but state doesn't, terraform
# apply fails with "already exists" / "DBInstanceAlreadyExists" / "ResourceAlreadyExistsException".
# We capture the apply log, detect those errors, import the orphaned resources,
# then retry once.  A second failure is fatal.

_TF_APPLY_ARGS=(
  -auto-approve
  -var "aws_profile=${AWS_PROFILE}"
  -var "aws_region=${AWS_REGION}"
  -var "app_name=${APP_NAME}"
  -var "environment=${ENVIRONMENT}"
)

# Maps terraform resource address → AWS resource ID (resolved at apply time)
_try_import() {
  local tf_addr="$1"
  local aws_id="$2"
  log "  Importing ${tf_addr} → ${aws_id} ..."
  terraform -chdir="$TF_DIR" import "${_TF_APPLY_ARGS[@]:1}" "$tf_addr" "$aws_id" \
    && log "  Import OK." \
    || warn "  Import failed for ${tf_addr} — apply may still succeed if resource was just transient."
}

_handle_already_exists_errors() {
  local apply_log="$1"
  local imported=0

  # Aurora cluster
  if grep -q 'DBClusterAlreadyExistsFault\|cluster.*already exists' "$apply_log" 2>/dev/null; then
    _try_import "aws_rds_cluster.main" "${PREFIX}-cluster"
    imported=1
  fi

  # Aurora instance (count = 1, index 0)
  if grep -q 'DBInstanceAlreadyExists\|instance.*already exists' "$apply_log" 2>/dev/null; then
    _try_import "aws_rds_cluster_instance.main[0]" "${PREFIX}-instance-0"
    imported=1
  fi

  # Secrets Manager secret
  if grep -q 'already scheduled for deletion\|ResourceExistsException.*secret' "$apply_log" 2>/dev/null; then
    local pending_arn
    pending_arn=$(aws_cmd secretsmanager list-secrets \
      --include-planned-deletion \
      --filters "Key=name,Values=${PREFIX}/db-password" \
      --query 'SecretList[0].ARN' --output text 2>/dev/null || true)
    if [[ -n "$pending_arn" && "$pending_arn" != "None" ]]; then
      log "  Force-deleting still-pending secret before import..."
      aws_cmd secretsmanager delete-secret \
        --secret-id "$pending_arn" --force-delete-without-recovery || true
      sleep 5
    fi
    imported=1
  fi

  # Lambda functions — scan for any "already exists" on a known lambda name
  for fn_key in authorizer orgs auth inboxes threads messages drafts webhooks search \
                ws_connect ws_disconnect email_inbound email_outbound \
                event_dispatcher_webhook event_dispatcher_ws embedder \
                ses_events scheduler_drafts domains; do
    local fn_name
    case "$fn_key" in
      ws_connect)               fn_name="${PREFIX}-ws-connect" ;;
      ws_disconnect)            fn_name="${PREFIX}-ws-disconnect" ;;
      email_inbound)            fn_name="${PREFIX}-email-inbound" ;;
      email_outbound)           fn_name="${PREFIX}-email-outbound" ;;
      event_dispatcher_webhook) fn_name="${PREFIX}-event-dispatcher-webhook" ;;
      event_dispatcher_ws)      fn_name="${PREFIX}-event-dispatcher-ws" ;;
      ses_events)               fn_name="${PREFIX}-ses-events" ;;
      scheduler_drafts)         fn_name="${PREFIX}-scheduler-drafts" ;;
      *)                        fn_name="${PREFIX}-${fn_key}" ;;
    esac
    if grep -q "${fn_name}.*already exist\|Function already exist" "$apply_log" 2>/dev/null; then
      _try_import "aws_lambda_function.${fn_key}" "$fn_name"
      imported=1
    fi
  done

  return $((imported == 0 ? 1 : 0))
}

_run_tf_apply() {
  local apply_log
  apply_log=$(mktemp /tmp/tf-apply-XXXXXX.log)

  log "Running terraform apply (this takes 20–35 minutes)..."
  set +e
  terraform -chdir="$TF_DIR" apply "${_TF_APPLY_ARGS[@]}" 2>&1 | tee "$apply_log"
  local tf_exit=${PIPESTATUS[0]}
  set -e

  if [[ $tf_exit -eq 0 ]]; then
    rm -f "$apply_log"
    return 0
  fi

  # Apply failed — check if it's an AlreadyExists class of error we can recover from
  warn "terraform apply exited $tf_exit — scanning for recoverable AlreadyExists errors..."
  if _handle_already_exists_errors "$apply_log"; then
    log "Imported orphaned resources. Retrying terraform apply (attempt 2/2)..."
    rm -f "$apply_log"
    # Second attempt: no PIPESTATUS capture needed — let set -e propagate failure
    terraform -chdir="$TF_DIR" apply "${_TF_APPLY_ARGS[@]}"
  else
    warn "No known AlreadyExists patterns found — apply failed for a different reason."
    rm -f "$apply_log"
    return $tf_exit
  fi
}

_run_tf_apply

# Post-apply: verify the bastion → RDS proxy egress SG rule exists.
# This rule is tracked via aws_security_group_rule (separate from the SG resource)
# and can get into a state-drift loop during interrupted applies.  We check AWS
# directly (ground truth) and add the rule if it is missing, regardless of state.
log "Verifying bastion → RDS proxy security group rule..."
BASTION_SG_ID=$(aws_cmd ec2 describe-security-groups \
  --filters "Name=group-name,Values=${PREFIX}-bastion" \
  --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || true)
RDS_PROXY_SG_ID=$(aws_cmd ec2 describe-security-groups \
  --filters "Name=group-name,Values=${PREFIX}-rds-proxy" \
  --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || true)
if [[ -n "$BASTION_SG_ID" && "$BASTION_SG_ID" != "None" && \
      -n "$RDS_PROXY_SG_ID" && "$RDS_PROXY_SG_ID" != "None" ]]; then
  existing_port=$(aws_cmd ec2 describe-security-groups \
    --group-ids "$BASTION_SG_ID" \
    --query 'SecurityGroups[0].IpPermissionsEgress[?FromPort==`5432`].FromPort' \
    --output text 2>/dev/null || true)
  if [[ "$existing_port" != "5432" ]]; then
    log "  Port 5432 egress missing — adding rule directly to AWS (state drift workaround)..."
    aws_cmd ec2 authorize-security-group-egress \
      --group-id "$BASTION_SG_ID" \
      --ip-permissions "IpProtocol=tcp,FromPort=5432,ToPort=5432,UserIdGroupPairs=[{GroupId=${RDS_PROXY_SG_ID},Description=PostgreSQL to RDS Proxy}]" \
      >/dev/null 2>&1 \
      || warn "Could not add bastion → proxy egress rule (may already exist or SGs not ready yet)"
  else
    log "  Bastion → RDS proxy egress rule OK."
  fi
fi

# Post-apply: verify the RDS proxy target is registered and healthy.
# After a partial apply, the proxy may exist but have no Aurora cluster registered
# in its target group, causing every connection to close immediately.
# Import the target if it is missing from state, then wait for AVAILABLE.
log "Verifying RDS proxy target health..."
PROXY_NAME="${PREFIX}-proxy"
CLUSTER_ID="${PREFIX}-cluster"
PROXY_TARGET_TIMEOUT=1800  # 30 min max (proxy target can take several minutes)
PROXY_TARGET_ELAPSED=0

# Check if aws_db_proxy_target.main is already in state
if ! terraform -chdir="$TF_DIR" state show "aws_db_proxy_target.main" &>/dev/null; then
  log "  aws_db_proxy_target.main not in state — importing..."
  _try_import "aws_db_proxy_target.main" "${PROXY_NAME}/default/${CLUSTER_ID}" || true
fi

log "  Waiting for proxy target to reach AVAILABLE (timeout: ${PROXY_TARGET_TIMEOUT}s)..."
_proxy_target_applied=false
while true; do
  target_state=$(aws_cmd rds describe-db-proxy-targets \
    --db-proxy-name "$PROXY_NAME" \
    --query 'Targets[0].TargetHealth.State' \
    --output text 2>/dev/null || echo "NOT_FOUND")

  if [[ "$target_state" == "AVAILABLE" ]]; then
    log "  Proxy target is AVAILABLE."
    break
  elif [[ "$target_state" == "NOT_FOUND" || "$target_state" == "None" || -z "$target_state" ]] && \
       [[ "$_proxy_target_applied" == "false" ]]; then
    log "  Proxy target not found — attempting targeted apply for proxy target..."
    terraform -chdir="$TF_DIR" apply "${_TF_APPLY_ARGS[@]}" \
      -target=aws_db_proxy_target.main \
      || warn "  Targeted apply for proxy target failed (non-fatal, will retry wait)"
    _proxy_target_applied=true
  else
    log "  Proxy target state: ${target_state} (waiting...)"
  fi

  if [[ $PROXY_TARGET_ELAPSED -ge $PROXY_TARGET_TIMEOUT ]]; then
    warn "Proxy target did not reach AVAILABLE within ${PROXY_TARGET_TIMEOUT}s — proceeding anyway."
    break
  fi
  sleep 30
  PROXY_TARGET_ELAPSED=$((PROXY_TARGET_ELAPSED + 30))
done

log "STEP 2 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 3 — Generate post-deploy environment
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 3: Sync post-deploy environment ==="

"${REPO_ROOT}/scripts/sync-env.sh" --profile "$AWS_PROFILE" --region "$AWS_REGION"

# Source the freshly written .env.outputs
set -a
# shellcheck disable=SC1090
source <(grep -v '^#' "${REPO_ROOT}/.env.outputs" | grep -v '^$' | sed 's/[[:space:]]*#.*$//')
set +a

log "Key values:"
log "  REST_API_ENDPOINT     : ${REST_API_ENDPOINT:-<not set>}"
log "  RDS_PROXY_ENDPOINT    : ${RDS_PROXY_ENDPOINT:-<not set>}"
log "  DATABASE_URL          : set (not printed)"

# RDS_PROXY_ENDPOINT may not be in older .env.outputs — fall back to terraform output
if [[ -z "${RDS_PROXY_ENDPOINT:-}" ]]; then
  RDS_PROXY_ENDPOINT=$(terraform -chdir="$TF_DIR" output -raw rds_proxy_endpoint 2>/dev/null || true)
  log "  RDS_PROXY_ENDPOINT (from tf output): ${RDS_PROXY_ENDPOINT:-<not set>}"
fi

[[ -n "${RDS_PROXY_ENDPOINT:-}" ]] || die "RDS_PROXY_ENDPOINT is empty — check terraform outputs."

log "STEP 3 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 4 — Wait for bastion SSM readiness
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 4: Wait for bastion EC2 to be SSM-reachable ==="

BASTION_ID=$(aws_cmd ec2 describe-instances \
  --filters \
    "Name=tag:Name,Values=${PREFIX}-bastion" \
    "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].InstanceId' \
  --output text 2>/dev/null || true)

[[ -n "$BASTION_ID" && "$BASTION_ID" != "None" ]] \
  || die "Bastion instance not found. Expected tag Name=${PREFIX}-bastion in running state."

log "Bastion instance ID: $BASTION_ID"
log "Waiting for SSM agent to register (up to 10 minutes)..."

ELAPSED=0
MAX_WAIT=600   # 10 minutes
while true; do
  SSM_STATUS=$(aws_cmd ssm describe-instance-information \
    --filters "Key=InstanceIds,Values=${BASTION_ID}" \
    --query 'InstanceInformationList[0].PingStatus' \
    --output text 2>/dev/null || echo "Unknown")

  if [[ "$SSM_STATUS" == "Online" ]]; then
    log "  SSM agent is Online."
    break
  fi

  ELAPSED=$((ELAPSED + 15))
  if [[ $ELAPSED -ge $MAX_WAIT ]]; then
    die "Timed out waiting for bastion SSM agent (${MAX_WAIT}s). Try running migrations manually."
  fi
  echo "  ... SSM status: ${SSM_STATUS} (${ELAPSED}s elapsed)"
  sleep 15
done

log "STEP 4 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 5 — Database migrations via SSM tunnel
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 5: Run database migrations ==="

# Get DB credentials from Secrets Manager
DB_SECRET_ARN=$(terraform -chdir="$TF_DIR" output -raw db_secret_arn 2>/dev/null)
[[ -n "$DB_SECRET_ARN" ]] || die "Could not read db_secret_arn from terraform output."

DB_PASSWORD=$(aws_cmd secretsmanager get-secret-value \
  --secret-id "$DB_SECRET_ARN" \
  --query SecretString --output text | jq -r '.password')

DB_USERNAME="${TF_VAR_db_username:-$(grep -E '^TF_VAR_db_username=' "${REPO_ROOT}/.env" 2>/dev/null | cut -d= -f2- || echo 'ngxadmin')}"
DB_NAME="${TF_VAR_db_name:-$(grep -E '^TF_VAR_db_name=' "${REPO_ROOT}/.env" 2>/dev/null | cut -d= -f2- || echo 'ngx')}"
TUNNEL_PORT=15432

TUNNEL_DATABASE_URL="postgres://${DB_USERNAME}:${DB_PASSWORD}@127.0.0.1:${TUNNEL_PORT}/${DB_NAME}?sslmode=verify-ca&sslrootcert=/usr/local/etc/openssl@3/cert.pem"

log "Opening SSM port-forwarding tunnel: localhost:${TUNNEL_PORT} → ${RDS_PROXY_ENDPOINT}:5432"
aws_cmd ssm start-session \
  --target "$BASTION_ID" \
  --document-name AWS-StartPortForwardingSessionToRemoteHost \
  --parameters "{\"host\":[\"${RDS_PROXY_ENDPOINT}\"],\"portNumber\":[\"5432\"],\"localPortNumber\":[\"${TUNNEL_PORT}\"]}" \
  &>/dev/null &
SSM_TUNNEL_PID=$!

# Wait for the tunnel to be ready (PostgreSQL TCP handshake)
log "Waiting for tunnel to be ready..."
TUNNEL_WAIT=0
while ! nc -z 127.0.0.1 "$TUNNEL_PORT" 2>/dev/null; do
  sleep 2
  TUNNEL_WAIT=$((TUNNEL_WAIT + 2))
  [[ $TUNNEL_WAIT -lt 60 ]] || die "SSM tunnel did not open on port ${TUNNEL_PORT} within 60s."
done
log "  Tunnel is ready."

# Run migrations directly (bypass Makefile's DB_ENV which reads .env.outputs)
log "Running database migrations..."
cd "$REPO_ROOT"
DATABASE_URL="$TUNNEL_DATABASE_URL" go run ./tools/migrate up

log "Migrations complete."

# Close the tunnel — cleanup trap will do it, but be explicit here
log "Closing SSM tunnel..."
kill "$SSM_TUNNEL_PID" 2>/dev/null || true
SSM_TUNNEL_PID=""

log "STEP 5 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 6 — Deploy Lambda code
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 6: Deploy Lambda code ==="

cd "$REPO_ROOT"
make deploy-lambdas

log "STEP 6 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 7 — Bootstrap initial org
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 7: Bootstrap initial org ==="

if [[ "$SKIP_BOOTSTRAP" == "true" ]]; then
  log "  --skip-bootstrap set — skipping."
else
  # Prompt for org/slug if not provided via CLI flags.
  # Skip the prompt when stdin is not a terminal (non-interactive / nohup run).
  if [[ -z "$ORG_NAME" ]]; then
    if [[ -t 0 ]]; then
      read -r -p "  Organisation name (or press Enter to skip bootstrap): " ORG_NAME
    else
      log "  Non-interactive run — skipping org bootstrap prompt."
    fi
  fi

  if [[ -z "$ORG_NAME" ]]; then
    log "  No org name provided — skipping bootstrap."
    log "  Run manually later: make bootstrap org='My Org' slug='my-org'"
  else
    if [[ -z "$ORG_SLUG" ]]; then
      # Default slug: lowercase org name, spaces → dashes
      ORG_SLUG=$(echo "$ORG_NAME" | tr '[:upper:]' '[:lower:]' | sed 's/ /-/g' | sed 's/[^a-z0-9-]//g')
      log "  Derived slug: ${ORG_SLUG}"
    fi

    # Open SSM tunnel again for bootstrap
    log "  Opening SSM tunnel for bootstrap..."
    aws_cmd ssm start-session \
      --target "$BASTION_ID" \
      --document-name AWS-StartPortForwardingSessionToRemoteHost \
      --parameters "{\"host\":[\"${RDS_PROXY_ENDPOINT}\"],\"portNumber\":[\"5432\"],\"localPortNumber\":[\"${TUNNEL_PORT}\"]}" \
      &>/dev/null &
    SSM_TUNNEL_PID=$!

    TUNNEL_WAIT=0
    while ! nc -z 127.0.0.1 "$TUNNEL_PORT" 2>/dev/null; do
      sleep 2
      TUNNEL_WAIT=$((TUNNEL_WAIT + 2))
      [[ $TUNNEL_WAIT -lt 60 ]] || die "SSM tunnel did not open on port ${TUNNEL_PORT} within 60s."
    done

    log "  Creating org '${ORG_NAME}' (slug: ${ORG_SLUG})..."
    cd "$REPO_ROOT"
    DATABASE_URL="$TUNNEL_DATABASE_URL" go run ./tools/bootstrap \
      -org "${ORG_NAME}" \
      -slug "${ORG_SLUG}" \
      | tee /dev/stderr 2>&1 | grep -E 'Key:|API key|am_live_' || true

    kill "$SSM_TUNNEL_PID" 2>/dev/null || true
    SSM_TUNNEL_PID=""

    echo
    log "  *** Save the API key printed above — it will not be shown again. ***"
  fi
fi

log "STEP 7 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 8 — Print post-apply DNS records
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 8: Post-apply DNS records ==="

MAIL_DOMAIN="${TF_VAR_mail_domain:-$(grep -E '^TF_VAR_mail_domain=' "${REPO_ROOT}/.env" | cut -d= -f2-)}"

log "Add the following DNS records at your DNS provider for: ${MAIL_DOMAIN}"
echo
echo "  ── SES domain verification (TXT) ──────────────────────────────────"
VERIFY_TOKEN=$(terraform -chdir="$TF_DIR" output -raw ses_verification_token 2>/dev/null || echo "<run: terraform output ses_verification_token>")
echo "  Type : TXT"
echo "  Host : _amazonses.${MAIL_DOMAIN}"
echo "  Value: ${VERIFY_TOKEN}"
echo

echo "  ── DKIM CNAME records (×3) ─────────────────────────────────────────"
DKIM_TOKENS=$(terraform -chdir="$TF_DIR" output -json ses_dkim_tokens 2>/dev/null \
  | jq -r '.[]' 2>/dev/null || echo "")

if [[ -n "$DKIM_TOKENS" ]]; then
  while IFS= read -r token; do
    echo "  Type : CNAME"
    echo "  Host : ${token}._domainkey.${MAIL_DOMAIN}"
    echo "  Value: ${token}.dkim.amazonses.com"
    echo
  done <<< "$DKIM_TOKENS"
else
  echo "  (run: terraform -chdir=terraform output ses_dkim_tokens)"
  echo
fi

echo "  ── Poll for SES verification ────────────────────────────────────────"
echo "  aws ses get-identity-verification-attributes \\"
echo "    --profile ${AWS_PROFILE} --region ${AWS_REGION} \\"
echo "    --identities ${MAIL_DOMAIN} \\"
echo "    --query 'VerificationAttributes.*.VerificationStatus'"
echo "  # Wait for: [\"Success\"]"
echo

log "STEP 8 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 9 — Smoke test
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 9: Smoke test ==="

if [[ "$SKIP_SMOKE" == "true" ]]; then
  log "  --skip-smoke set — skipping."
else
  [[ -n "${REST_API_ENDPOINT:-}" ]] \
    || die "REST_API_ENDPOINT is not set. Run: source loadenv.sh"

  log "  Checking API Gateway health: ${REST_API_ENDPOINT}/v1/org"
  HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" \
    --connect-timeout 10 --max-time 30 \
    "${REST_API_ENDPOINT}/v1/org" \
    -H "Authorization: Bearer placeholder") || true

  case "$HTTP_STATUS" in
    200|401|403)
      log "  API Gateway reachable — HTTP ${HTTP_STATUS} (expected: 401 without valid key)"
      ;;
    000)
      warn "  Could not reach ${REST_API_ENDPOINT} — check API Gateway deployment."
      ;;
    *)
      warn "  Unexpected HTTP status: ${HTTP_STATUS} — investigate before proceeding."
      ;;
  esac
fi

log "STEP 9 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# Done
# ─────────────────────────────────────────────────────────────────────────────
echo "============================================================"
log "=== ${PREFIX} environment setup complete ==="
echo "============================================================"
echo
echo "Next steps:"
echo "  1. Add DNS records printed in STEP 8 above."
echo "  2. Wait for SES domain verification (5–15 min)."
echo "  3. Source the environment:  source loadenv.sh"
echo "  4. Run a full smoke test:   see docs/runbook-deployment.md §A10"
if [[ "$SKIP_BOOTSTRAP" != "true" && -z "$ORG_NAME" ]]; then
  echo "  5. Bootstrap org:           make bootstrap org='My Org' slug='my-org'"
fi
echo
