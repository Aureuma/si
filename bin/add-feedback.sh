#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: add-feedback.sh <severity> <message> [source] [context]" >&2
  echo "severity: info|warn|error" >&2
  echo "env: MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

SEV="$1"
MESSAGE="$2"
SOURCE="${3:-}"
CONTEXT="${4:-}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

esc() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\\"/g'
}

PAYLOAD=$(printf '{"severity":"%s","message":"%s","source":"%s","context":"%s"}' \
  "$(esc "$SEV")" \
  "$(esc "$MESSAGE")" \
  "$(esc "$SOURCE")" \
  "$(esc "$CONTEXT")")

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "$PAYLOAD" \
  "$MANAGER_URL/feedback"
