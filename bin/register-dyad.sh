#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: register-dyad.sh <name> [role] [department]" >&2
}

if [[ $# -lt 1 ]]; then
  usage
  exit 1
fi

NAME="$1"
ROLE="${2:-generic}"
DEPT="${3:-$ROLE}"
MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"

if ! [[ "$NAME" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid dyad name: $NAME (allowed: letters, numbers, _ and -)" >&2
  exit 1
fi
if ! [[ "$ROLE" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid role: $ROLE (allowed: letters, numbers, _ and -)" >&2
  exit 1
fi
if ! [[ "$DEPT" =~ ^[A-Za-z0-9_-]+$ ]]; then
  echo "invalid department: $DEPT (allowed: letters, numbers, _ and -)" >&2
  exit 1
fi
if [[ -z "$MANAGER_URL" ]]; then
  echo "MANAGER_URL is required to register dyads" >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo "curl is required to register dyads" >&2
  exit 1
fi

payload=$(printf '{"dyad":"%s","department":"%s","role":"%s","available":true}' "$NAME" "$DEPT" "$ROLE")
if ! curl -fsS -X POST -H "Content-Type: application/json" -d "$payload" "${MANAGER_URL%/}/dyads" >/dev/null; then
  echo "failed to register dyad '$NAME' at ${MANAGER_URL}" >&2
  exit 1
fi

echo "registered dyad '$NAME' (role=$ROLE dept=$DEPT) at ${MANAGER_URL}"
