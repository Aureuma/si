#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

AGE_KEY_FILE="${ROOT_DIR}/secrets/age.key"
TOKEN_FILE="${ROOT_DIR}/secrets/credentials_mcp_token"

if [[ ! -f "$AGE_KEY_FILE" ]]; then
  "${ROOT_DIR}/bin/sops-init.sh"
fi

if [[ ! -f "$TOKEN_FILE" ]]; then
  umask 077
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32 >"$TOKEN_FILE"
  else
    head -c 32 /dev/urandom | base64 >"$TOKEN_FILE"
  fi
fi

kube create secret generic silexa-credentials-secrets \
  --from-file=age.key="$AGE_KEY_FILE" \
  --from-file=credentials_mcp_token="$TOKEN_FILE" \
  --dry-run=client -o yaml | kube apply -f -

echo "applied secret silexa-credentials-secrets"

shopt -s nullglob
files=("${ROOT_DIR}"/secrets/*.sops.*)
if [[ ${#files[@]} -eq 0 ]]; then
  echo "no encrypted secret files found under ${ROOT_DIR}/secrets"
  exit 0
fi

args=()
for file in "${files[@]}"; do
  name=$(basename "$file")
  args+=("--from-file=${name}=${file}")
done

kube create configmap silexa-credentials-encrypted \
  "${args[@]}" \
  --dry-run=client -o yaml | kube apply -f -

echo "applied configmap silexa-credentials-encrypted"
