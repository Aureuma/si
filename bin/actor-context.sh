#!/usr/bin/env bash
set -euo pipefail

# Print the starting context for a given actor/critic profile.
# Usage: actor-context.sh <profile-name>
# Profiles live under ./profiles and are named <profile>.md

if [[ $# -lt 1 ]]; then
  echo "usage: actor-context.sh <profile-name>" >&2
  exit 1
fi

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PROFILE="$1"
FILE="${ROOT}/profiles/${PROFILE}.md"

if [[ ! -f "$FILE" ]]; then
  echo "profile not found: ${FILE}" >&2
  exit 1
fi

cat "$FILE"
