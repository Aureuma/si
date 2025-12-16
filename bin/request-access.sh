#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: request-access.sh <requester> <resource> <action> [reason] [department]" >&2
  echo "env: MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

REQ="$1"
RESOURCE="$2"
ACTION="$3"
REASON="${4:-}"
DEPT="${5:-}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\\"/g'; }

PAYLOAD=$(printf '{"requester":"%s","resource":"%s","action":"%s","reason":"%s","department":"%s"}' \
  "$(esc "$REQ")" "$(esc "$RESOURCE")" "$(esc "$ACTION")" "$(esc "$REASON")" "$(esc "$DEPT")")

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "$PAYLOAD" \
  "$MANAGER_URL/access-requests"
