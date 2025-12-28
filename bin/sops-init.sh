#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEY_FILE="${ROOT_DIR}/secrets/age.key"
SOPS_CONFIG="${ROOT_DIR}/.sops.yaml"

if ! command -v age-keygen >/dev/null 2>&1; then
  echo "age-keygen not found. Install age before running this script." >&2
  exit 1
fi
if ! command -v sops >/dev/null 2>&1; then
  echo "sops not found. Install sops before running this script." >&2
  exit 1
fi

mkdir -p "${ROOT_DIR}/secrets"
if [[ ! -f "$KEY_FILE" ]]; then
  age-keygen -o "$KEY_FILE"
  chmod 600 "$KEY_FILE" || true
fi

pub=$(age-keygen -y "$KEY_FILE" | tr -d '\n')
if [[ -z "$pub" ]]; then
  echo "failed to read age public key from ${KEY_FILE}" >&2
  exit 1
fi

if [[ -f "$SOPS_CONFIG" ]]; then
  if grep -q "age1REPLACE_WITH_YOUR_PUBLIC_KEY" "$SOPS_CONFIG"; then
    sed -i "s/age1REPLACE_WITH_YOUR_PUBLIC_KEY/${pub}/g" "$SOPS_CONFIG"
  fi
else
  cat >"$SOPS_CONFIG" <<EOF
creation_rules:
  - path_regex: secrets/.*\\.sops\\.(env|yaml|yml|json)$
    age:
      - "${pub}"
EOF
fi

echo "SOPS initialized."
echo "Age key: ${KEY_FILE}"
echo "Public key: ${pub}"
echo "Export SOPS_AGE_KEY_FILE=${KEY_FILE} before decrypting."
