#!/usr/bin/env bash
set -euo pipefail

# Post a single metric to the manager.
# usage: post-metric.sh <dyad> <department> <name> <value> [unit] [recorded_by]
# env: MANAGER_URL (default http://localhost:9090)

if [[ $# -lt 4 ]]; then
  echo "usage: post-metric.sh <dyad> <department> <name> <value> [unit] [recorded_by]" >&2
  exit 1
fi

DYAD="$1"
DEPT="$2"
NAME="$3"
VALUE="$4"
UNIT="${5:-count}"
RECORDED_BY="${6:-manual}"
MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"

PAYLOAD=$(cat <<EOF
{
  "dyad": "${DYAD}",
  "department": "${DEPT}",
  "name": "${NAME}",
  "value": ${VALUE},
  "unit": "${UNIT}",
  "recorded_by": "${RECORDED_BY}"
}
EOF
)

curl -fsSL -X POST -H "Content-Type: application/json" \
  -d "${PAYLOAD}" \
  "${MANAGER_URL}/metrics"
echo "Posted metric ${NAME}=${VALUE} for ${DYAD}/${DEPT}"
