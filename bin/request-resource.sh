#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 ]]; then
  echo "usage: request-resource.sh <resource> <action> <payload> [requested_by] [notes]" >&2
  echo "env: BROKER_URL (default http://localhost:9091)" >&2
  exit 1
fi

RESOURCE="$1"
ACTION="$2"
PAYLOAD="$3"
REQUESTED_BY="${4:-}"
NOTES="${5:-}"
BROKER_URL=${BROKER_URL:-http://localhost:9091}

esc() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

PAYLOAD_JSON=$(printf '{"resource":"%s","action":"%s","payload":"%s","requested_by":"%s","notes":"%s"}' \
  "$(esc "$RESOURCE")" "$(esc "$ACTION")" "$(esc "$PAYLOAD")" "$(esc "$REQUESTED_BY")" "$(esc "$NOTES")")

curl -fsSL -X POST -H "Content-Type: application/json" -d "$PAYLOAD_JSON" "$BROKER_URL/requests"
