#!/usr/bin/env bash
set -euo pipefail

# Beam helper: create a `beam.dyad_bootstrap` dyad task.
# Temporal Beam workflow will create PVC + Deployment and wait for readiness.
#
# Usage: beam-dyad-bootstrap.sh <dyad> [role] [department]
# Env:
#   MANAGER_URL (default http://localhost:9090)
#   REQUESTED_BY (default beam-dyad-bootstrap)
#   ACTOR (default actor)
#   CRITIC (default critic)

if [[ $# -lt 1 ]]; then
  echo "usage: beam-dyad-bootstrap.sh <dyad> [role] [department]" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

DYAD="$1"
ROLE="${2:-generic}"
DEPT="${3:-$ROLE}"

MANAGER_URL="${MANAGER_URL:-http://localhost:9090}"
REQUESTED_BY="${REQUESTED_BY:-beam-dyad-bootstrap}"
ACTOR="${ACTOR:-actor}"
CRITIC="${CRITIC:-critic}"

NOTES="[beam.dyad_bootstrap.role]=${ROLE}"$'\n'"[beam.dyad_bootstrap.department]=${DEPT}"

TITLE="Beam: Bootstrap dyad ${DYAD}"
DESC="Temporal Beam workflow will provision the dyad deployment and wait for readiness."

DYAD_TASK_KIND="beam.dyad_bootstrap" \
REQUESTED_BY="${REQUESTED_BY}" \
MANAGER_URL="${MANAGER_URL}" \
  "${ROOT_DIR}/bin/add-dyad-task.sh" \
  "${TITLE}" \
  "${DYAD}" \
  "${ACTOR}" \
  "${CRITIC}" \
  "high" \
  "${DESC}" \
  "" \
  "${NOTES}"

echo "beam.dyad_bootstrap task created for dyad ${DYAD}"
