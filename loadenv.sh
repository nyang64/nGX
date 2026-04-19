set -a                                                                                                                
source <(grep -v '^#' .env | grep -v '^$' | sed 's/[[:space:]]*#.*$//')
set +a                                                                                                                
export DKIM_PRIVATE_KEY_PEM="$(cat configs/dkim.pem)"
