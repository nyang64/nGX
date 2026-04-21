set -a
source <(grep -v '^#' .env | grep -v '^$' | sed 's/[[:space:]]*#.*$//')
# Post-deploy outputs override same-named vars from .env.
# Generate with: scripts/sync-env.sh
[ -f .env.outputs ] && source <(grep -v '^#' .env.outputs | grep -v '^$' | sed 's/[[:space:]]*#.*$//')
set +a
# TF_VAR_* variables in .env are automatically exported above and picked up
# by Terraform without any -var flags or terraform.tfvars file.
# Usage: source loadenv.sh && AWS_PROFILE=<profile> terraform plan
