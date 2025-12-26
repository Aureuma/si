#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

FORCE=false
if [[ "${1:-}" == "--force" ]]; then
  FORCE=true
  shift
fi

declare -A SECRET_FILES
SECRET_FILES[telegram_bot_token]="${ROOT_DIR}/secrets/telegram_bot_token"
SECRET_FILES[gh_token]="${ROOT_DIR}/secrets/gh_token"
SECRET_FILES[stripe_api_key]="${ROOT_DIR}/secrets/stripe_api_key"

secrets=("${@}")
if [[ ${#secrets[@]} -eq 0 ]]; then
  secrets=("${!SECRET_FILES[@]}")
fi

create_secret() {
  local name="$1"
  local file="${SECRET_FILES[$name]:-}"

  if [[ -z "$file" ]]; then
    echo "unknown secret: $name" >&2
    return 1
  fi

  if [[ "$FORCE" == "true" ]]; then
    docker secret rm "$name" >/dev/null 2>&1 || true
  fi

  if docker secret inspect "$name" >/dev/null 2>&1; then
    echo "secret ${name} exists"
    return 0
  fi

  if [[ "$name" == "telegram_bot_token" ]]; then
    if [[ ! -s "$file" ]]; then
      echo "missing ${file}; write telegram bot token first" >&2
      return 1
    fi
    docker secret create "$name" "$file" >/dev/null
    echo "created secret ${name}"
    return 0
  fi

  if [[ -f "$file" ]]; then
    docker secret create "$name" "$file" >/dev/null
    echo "created secret ${name}"
    return 0
  fi

  echo "warning: ${file} missing; creating empty secret ${name}" >&2
  printf '' | docker secret create "$name" - >/dev/null
  echo "created secret ${name} (empty)"
}

for secret in "${secrets[@]}"; do
  create_secret "$secret"
done
