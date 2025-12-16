#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: resolve-access.sh <id> <approved|denied> [resolved_by] [notes]" >&2
  echo "env: MANAGER_URL (default http://localhost:9090)" >&2
  exit 1
fi

ID="$1"
STATUS="$2"
BY="${3:-}"
NOTES="${4:-}"
MANAGER_URL=${MANAGER_URL:-http://localhost:9090}

curl -fsSL -X POST "$MANAGER_URL/access-requests/resolve?id=${ID}&status=${STATUS}&by=$(printf %s "$BY" | sed 's/ /%20/g')&notes=$(printf %s "$NOTES" | sed 's/ /%20/g')"
