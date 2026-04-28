#!/usr/bin/env bash
# destroy-env.sh — Completely wipe an nGX environment from AWS.
#
# Order of operations:
#   1. Pre-flight checks
#   2. Manually delete RDS Proxy → Aurora instance → Aurora cluster + snapshots
#   3. Drain & delete all S3 versioned objects, delete markers, then buckets
#   4. Remove the above from Terraform state, then terraform destroy -refresh=false
#   5. Verify nothing remains
#
# Usage:
#   ./scripts/destroy-env.sh [--profile <aws-profile>] [--region <region>] \
#                             [--app <app_name>] [--env <environment>] \
#                             [--tf-dir <path-to-terraform-dir>] [--yes]
#
# Defaults match the current prod stack:
#   profile = nyk-tf   region = us-east-1   app = ngx   env = prod
#
# Pass --yes to skip the confirmation prompt (use in automation with care).

set -euo pipefail

# ─── Defaults ────────────────────────────────────────────────────────────────
AWS_PROFILE="${AWS_PROFILE:-nyk-tf}"
AWS_REGION="us-east-1"
APP_NAME="ngx"
ENVIRONMENT="prod"
TF_DIR="$(cd "$(dirname "$0")/../terraform" && pwd)"
AUTO_APPROVE=false

# ─── Argument parsing ────────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)  AWS_PROFILE="$2";  shift 2 ;;
    --region)   AWS_REGION="$2";   shift 2 ;;
    --app)      APP_NAME="$2";     shift 2 ;;
    --env)      ENVIRONMENT="$2";  shift 2 ;;
    --tf-dir)   TF_DIR="$2";       shift 2 ;;
    --yes)      AUTO_APPROVE=true; shift   ;;
    *) echo "Unknown option: $1"; exit 1   ;;
  esac
done

PREFIX="${APP_NAME}-${ENVIRONMENT}"

# Derived resource names (must match terraform locals.tf)
CLUSTER_ID="${PREFIX}-cluster"
INSTANCE_ID="${PREFIX}-instance-0"
PROXY_NAME="${PREFIX}-proxy"
S3_EMAILS="${PREFIX}-emails"
S3_ATTACHMENTS="${PREFIX}-attachments"
S3_ARTIFACTS="${PREFIX}-lambda-artifacts"

# Terraform state resource addresses to remove after manual deletion
TF_AURORA_RESOURCES=(
  "aws_db_proxy_target.main"
  "aws_db_proxy_default_target_group.main"
  "aws_db_proxy.main"
  "aws_rds_cluster_instance.main"
  "aws_rds_cluster.main"
  "aws_rds_cluster_parameter_group.main"
  "aws_db_subnet_group.main"
  "aws_iam_role_policy.rds_proxy_secrets"
  "aws_iam_role.rds_proxy"
)
TF_S3_RESOURCES=(
  "aws_s3_bucket_notification.emails"
  "aws_s3_bucket_lifecycle_configuration.emails"
  "aws_s3_bucket_public_access_block.emails"
  "aws_s3_bucket_public_access_block.attachments"
  "aws_s3_bucket_public_access_block.artifacts"
  "aws_s3_bucket_server_side_encryption_configuration.emails"
  "aws_s3_bucket_server_side_encryption_configuration.attachments"
  "aws_s3_bucket_server_side_encryption_configuration.artifacts"
  "aws_s3_bucket_versioning.emails"
  "aws_s3_bucket_versioning.artifacts"
  "aws_s3_bucket.emails"
  "aws_s3_bucket.attachments"
  "aws_s3_bucket.artifacts"
)

# ─── Helpers ─────────────────────────────────────────────────────────────────
log()  { echo "[$(date -u +%H:%M:%S)] $*"; }
warn() { echo "[$(date -u +%H:%M:%S)] WARN: $*" >&2; }
die()  { echo "[$(date -u +%H:%M:%S)] ERROR: $*" >&2; exit 1; }

aws_cmd() {
  aws --profile "$AWS_PROFILE" --region "$AWS_REGION" "$@"
}

resource_exists() {
  # Returns 0 if the aws command returns non-empty, non-"None" output.
  local out
  out=$(aws_cmd "$@" 2>/dev/null || true)
  [[ -n "$out" && "$out" != "None" ]]
}

wait_for_deletion() {
  local desc="$1"; shift
  local max_minutes="${1:-20}"; shift 2>/dev/null || true
  local elapsed=0
  log "Waiting for $desc to be deleted (up to ${max_minutes}m)..."
  while resource_exists "$@"; do
    elapsed=$((elapsed + 15))
    if [[ $((elapsed / 60)) -ge $max_minutes ]]; then
      die "Timed out waiting for $desc to be deleted after ${max_minutes}m"
    fi
    echo "  ... still waiting (${elapsed}s elapsed)"
    sleep 15
  done
  log "$desc deleted."
}

# ─── Pre-flight ───────────────────────────────────────────────────────────────
log "=== nGX Environment Destroy Script ==="
log "  Profile   : $AWS_PROFILE"
log "  Region    : $AWS_REGION"
log "  Prefix    : $PREFIX"
log "  TF dir    : $TF_DIR"
echo

[[ -d "$TF_DIR" ]] || die "Terraform directory not found: $TF_DIR"

# Verify AWS credentials work
aws_cmd sts get-caller-identity --query 'Account' --output text > /dev/null \
  || die "AWS credentials not working for profile '$AWS_PROFILE'"

log "Pre-flight OK — AWS account: $(aws_cmd sts get-caller-identity --query 'Account' --output text)"
echo

# ─── Confirmation ─────────────────────────────────────────────────────────────
if [[ "$AUTO_APPROVE" != "true" ]]; then
  echo "WARNING: This will PERMANENTLY DESTROY all ${PREFIX} infrastructure:"
  echo "  - Aurora cluster:  $CLUSTER_ID"
  echo "  - Aurora instance: $INSTANCE_ID"
  echo "  - RDS Proxy:       $PROXY_NAME"
  echo "  - S3 buckets:      $S3_EMAILS  $S3_ATTACHMENTS  $S3_ARTIFACTS"
  echo "  - All Lambda functions, API Gateway, VPC, SQS, etc. via terraform destroy"
  echo
  read -r -p "Type the environment name to confirm destruction [${ENVIRONMENT}]: " confirm
  [[ "$confirm" == "$ENVIRONMENT" ]] || die "Confirmation did not match — aborting."
fi

echo
log ">>> Starting destruction of ${PREFIX} environment <<<"
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 1 — RDS Proxy + Aurora cluster + system snapshots
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 1: Delete RDS Proxy, Aurora instance, cluster, and snapshots ==="

## 1a. Delete RDS Proxy (if it exists)
if aws_cmd rds describe-db-proxies \
     --filters "Name=db-proxy-name,Values=${PROXY_NAME}" \
     --query 'DBProxies[0].DBProxyArn' --output text 2>/dev/null | grep -q '^arn:'; then

  log "Deleting RDS Proxy: $PROXY_NAME"
  aws_cmd rds delete-db-proxy --db-proxy-name "$PROXY_NAME" \
    || warn "delete-db-proxy failed (may already be deleting)"

  wait_for_deletion "RDS Proxy ${PROXY_NAME}" 20 \
    rds describe-db-proxies \
      --filters "Name=db-proxy-name,Values=${PROXY_NAME}" \
      --query 'DBProxies[0].DBProxyArn' --output text
else
  log "RDS Proxy $PROXY_NAME not found — skipping."
fi

## 1b. Delete Aurora cluster instance (if it exists)
if aws_cmd rds describe-db-instances \
     --db-instance-identifier "$INSTANCE_ID" \
     --query 'DBInstances[0].DBInstanceStatus' --output text 2>/dev/null | grep -qv "^$\|None"; then

  log "Deleting Aurora instance: $INSTANCE_ID"
  aws_cmd rds delete-db-instance \
    --db-instance-identifier "$INSTANCE_ID" \
    --skip-final-snapshot \
    || warn "delete-db-instance failed (may already be deleting)"

  wait_for_deletion "Aurora instance ${INSTANCE_ID}" 20 \
    rds describe-db-instances \
      --db-instance-identifier "$INSTANCE_ID" \
      --query 'DBInstances[0].DBInstanceStatus' --output text
else
  log "Aurora instance $INSTANCE_ID not found — skipping."
fi

## 1c. Disable deletion protection, then delete Aurora cluster
if aws_cmd rds describe-db-clusters \
     --db-cluster-identifier "$CLUSTER_ID" \
     --query 'DBClusters[0].Status' --output text 2>/dev/null | grep -qv "^$\|None"; then

  log "Disabling deletion protection on cluster: $CLUSTER_ID"
  aws_cmd rds modify-db-cluster \
    --db-cluster-identifier "$CLUSTER_ID" \
    --no-deletion-protection \
    --apply-immediately \
    || warn "modify-db-cluster failed"

  log "Deleting Aurora cluster: $CLUSTER_ID"
  aws_cmd rds delete-db-cluster \
    --db-cluster-identifier "$CLUSTER_ID" \
    --skip-final-snapshot \
    || warn "delete-db-cluster failed (may already be deleting)"

  wait_for_deletion "Aurora cluster ${CLUSTER_ID}" 30 \
    rds describe-db-clusters \
      --db-cluster-identifier "$CLUSTER_ID" \
      --query 'DBClusters[0].Status' --output text
else
  log "Aurora cluster $CLUSTER_ID not found — skipping."
fi

## 1d. Delete manual cluster snapshots
log "Deleting manual snapshots for cluster: $CLUSTER_ID"
manual_snapshot_ids=$(aws_cmd rds describe-db-cluster-snapshots \
  --db-cluster-identifier "$CLUSTER_ID" \
  --snapshot-type manual \
  --query 'DBClusterSnapshots[*].DBClusterSnapshotIdentifier' \
  --output text 2>/dev/null || true)

if [[ -n "$manual_snapshot_ids" && "$manual_snapshot_ids" != "None" ]]; then
  for snap_id in $manual_snapshot_ids; do
    log "  Deleting manual snapshot: $snap_id"
    aws_cmd rds delete-db-cluster-snapshot \
      --db-cluster-snapshot-identifier "$snap_id" \
      || warn "Failed to delete snapshot $snap_id"
  done
else
  log "  No manual snapshots found."
fi

## 1e. Delete retained automated backups ("system snapshots" in console).
# These use a different API from regular snapshots — describe-db-cluster-automated-backups.
log "Deleting retained automated backups for cluster: $CLUSTER_ID"
backup_resource_ids=$(aws_cmd rds describe-db-cluster-automated-backups \
  --db-cluster-identifier "$CLUSTER_ID" \
  --query 'DBClusterAutomatedBackups[*].DbClusterResourceId' \
  --output text 2>/dev/null || true)

if [[ -n "$backup_resource_ids" && "$backup_resource_ids" != "None" ]]; then
  for resource_id in $backup_resource_ids; do
    log "  Deleting retained automated backup: $resource_id"
    aws_cmd rds delete-db-cluster-automated-backup \
      --db-cluster-resource-id "$resource_id" \
      || warn "Failed to delete automated backup $resource_id"
  done
else
  log "  No retained automated backups found."
fi

log "STEP 1 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 2 — Drain and delete S3 buckets
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 2: Empty and delete S3 buckets ==="

drain_and_delete_bucket() {
  local bucket="$1"

  if ! aws_cmd s3api head-bucket --bucket "$bucket" 2>/dev/null; then
    log "  Bucket $bucket not found — skipping."
    return
  fi

  log "  Draining versioned objects from: $bucket"

  # Delete all object versions in batches of 1000
  local batch_count=0
  while true; do
    local versions_json
    versions_json=$(aws_cmd s3api list-object-versions \
      --bucket "$bucket" \
      --max-items 1000 \
      --query '{Objects: Versions[].{Key:Key,VersionId:VersionId}, NextToken: NextToken}' \
      --output json 2>/dev/null || echo '{"Objects":null}')

    local objects
    objects=$(echo "$versions_json" | python3 -c "
import sys, json
data = json.load(sys.stdin)
objs = data.get('Objects') or []
if objs:
    print(json.dumps({'Objects': objs, 'Quiet': True}))
" 2>/dev/null || true)

    if [[ -z "$objects" ]]; then break; fi

    batch_count=$((batch_count + 1))
    echo "$objects" | aws_cmd s3api delete-objects \
      --bucket "$bucket" \
      --delete "file:///dev/stdin" \
      --output json > /dev/null
    echo "    Deleted version batch #${batch_count}"
  done

  # Delete all delete markers in batches of 1000
  local marker_count=0
  while true; do
    local markers_json
    markers_json=$(aws_cmd s3api list-object-versions \
      --bucket "$bucket" \
      --max-items 1000 \
      --query '{Objects: DeleteMarkers[].{Key:Key,VersionId:VersionId}}' \
      --output json 2>/dev/null || echo '{"Objects":null}')

    local markers
    markers=$(echo "$markers_json" | python3 -c "
import sys, json
data = json.load(sys.stdin)
objs = data.get('Objects') or []
if objs:
    print(json.dumps({'Objects': objs, 'Quiet': True}))
" 2>/dev/null || true)

    if [[ -z "$markers" ]]; then break; fi

    marker_count=$((marker_count + 1))
    echo "$markers" | aws_cmd s3api delete-objects \
      --bucket "$bucket" \
      --delete "file:///dev/stdin" \
      --output json > /dev/null
    echo "    Deleted marker batch #${marker_count}"
  done

  log "  Deleting bucket: $bucket"
  aws_cmd s3api delete-bucket --bucket "$bucket" \
    || warn "delete-bucket failed for $bucket"
  log "  Bucket $bucket deleted."
}

for bucket in "$S3_EMAILS" "$S3_ATTACHMENTS" "$S3_ARTIFACTS"; do
  drain_and_delete_bucket "$bucket"
done

log "STEP 2 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 3 — Terraform destroy (state-rm + destroy -refresh=false)
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 3: Terraform destroy ==="

# 3a. Remove manually-deleted resources from Terraform state so destroy
#     doesn't try to call AWS APIs for them (they're already gone).
log "Removing Aurora and S3 resources from Terraform state..."

for addr in "${TF_AURORA_RESOURCES[@]}" "${TF_S3_RESOURCES[@]}"; do
  if terraform -chdir="$TF_DIR" state list 2>/dev/null | grep -qF "$addr"; then
    log "  terraform state rm $addr"
    terraform -chdir="$TF_DIR" state rm "$addr" || warn "state rm failed for $addr"
  else
    log "  $addr not in state — skipping"
  fi
done

# 3b. Destroy everything remaining.
log "Running terraform destroy -refresh=false..."
TF_VAR_aws_profile="$AWS_PROFILE" \
TF_VAR_aws_region="$AWS_REGION" \
TF_VAR_app_name="$APP_NAME" \
TF_VAR_environment="$ENVIRONMENT" \
  terraform -chdir="$TF_DIR" destroy \
    -refresh=false \
    -auto-approve \
    -var "aws_profile=${AWS_PROFILE}" \
    -var "aws_region=${AWS_REGION}" \
    -var "app_name=${APP_NAME}" \
    -var "environment=${ENVIRONMENT}"

log "STEP 3 complete."
echo

# ─────────────────────────────────────────────────────────────────────────────
# STEP 4 — Verification: assert nothing remains
# ─────────────────────────────────────────────────────────────────────────────
log "=== STEP 4: Verification ==="

ERRORS=0

## 4a. Lambda functions
log "Checking Lambda functions with prefix '${PREFIX}'..."
remaining_lambdas=$(aws_cmd lambda list-functions \
  --query "Functions[?starts_with(FunctionName,'${PREFIX}')].FunctionName" \
  --output text 2>/dev/null || true)

if [[ -n "$remaining_lambdas" && "$remaining_lambdas" != "None" ]]; then
  warn "FAIL: Lambda functions still exist: $remaining_lambdas"
  ERRORS=$((ERRORS + 1))
else
  log "  OK — no ${PREFIX} Lambda functions."
fi

## 4b. S3 buckets
log "Checking S3 buckets with prefix '${PREFIX}'..."
for bucket in "$S3_EMAILS" "$S3_ATTACHMENTS" "$S3_ARTIFACTS"; do
  if aws_cmd s3api head-bucket --bucket "$bucket" 2>/dev/null; then
    warn "FAIL: S3 bucket still exists: $bucket"
    ERRORS=$((ERRORS + 1))
  else
    log "  OK — bucket $bucket is gone."
  fi
done

## 4c. Aurora cluster
log "Checking Aurora cluster '${CLUSTER_ID}'..."
cluster_status=$(aws_cmd rds describe-db-clusters \
  --db-cluster-identifier "$CLUSTER_ID" \
  --query 'DBClusters[0].Status' --output text 2>/dev/null || echo "not-found")

if [[ "$cluster_status" != "not-found" && "$cluster_status" != "None" && -n "$cluster_status" ]]; then
  warn "FAIL: Aurora cluster still exists with status: $cluster_status"
  ERRORS=$((ERRORS + 1))
else
  log "  OK — Aurora cluster is gone."
fi

## 4d. Aurora instance
log "Checking Aurora instance '${INSTANCE_ID}'..."
instance_status=$(aws_cmd rds describe-db-instances \
  --db-instance-identifier "$INSTANCE_ID" \
  --query 'DBInstances[0].DBInstanceStatus' --output text 2>/dev/null || echo "not-found")

if [[ "$instance_status" != "not-found" && "$instance_status" != "None" && -n "$instance_status" ]]; then
  warn "FAIL: Aurora instance still exists with status: $instance_status"
  ERRORS=$((ERRORS + 1))
else
  log "  OK — Aurora instance is gone."
fi

## 4e. RDS Proxy
log "Checking RDS Proxy '${PROXY_NAME}'..."
proxy_status=$(aws_cmd rds describe-db-proxies \
  --filters "Name=db-proxy-name,Values=${PROXY_NAME}" \
  --query 'DBProxies[0].Status' --output text 2>/dev/null || echo "not-found")

if [[ "$proxy_status" != "not-found" && "$proxy_status" != "None" && -n "$proxy_status" ]]; then
  warn "FAIL: RDS Proxy still exists with status: $proxy_status"
  ERRORS=$((ERRORS + 1))
else
  log "  OK — RDS Proxy is gone."
fi

echo
if [[ $ERRORS -eq 0 ]]; then
  log "=== ALL CHECKS PASSED — ${PREFIX} environment fully destroyed ==="
else
  warn "=== ${ERRORS} check(s) FAILED — some resources may still exist ==="
  exit 1
fi
