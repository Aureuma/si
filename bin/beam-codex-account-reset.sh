#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: beam-codex-account-reset.sh <dyad> [targets] [paths] [reason]" >&2
  echo "targets: comma list (default actor,critic)" >&2
  echo "paths: comma list (default /root/.codex,/root/.config/openai-codex,/root/.config/codex,/root/.cache/openai-codex,/root/.cache/codex)" >&2
  exit 1
fi

DYAD="$1"
TARGETS="${2:-actor,critic}"
PATHS="${3:-/root/.codex,/root/.config/openai-codex,/root/.config/codex,/root/.cache/openai-codex,/root/.cache/codex}"
REASON="${4:-}"

NOTES="[beam.codex_account_reset.targets]=${TARGETS}"
NOTES+=$'\n'"[beam.codex_account_reset.paths]=${PATHS}"
if [[ -n "$REASON" ]]; then
  NOTES+=$'\n'"[beam.codex_account_reset.reason]=${REASON}"
fi

TITLE="Beam: Reset Codex account for ${DYAD}"
DESC="Clear Codex CLI state (auth/config/cache) in the dyad so it can log into a new account."

DYAD_TASK_KIND="beam.codex_account_reset" \
REQUESTED_BY="beam-codex-account-reset" \
  "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/bin/add-dyad-task.sh" \
  "${TITLE}" \
  "${DYAD}" \
  "actor" \
  "critic" \
  "high" \
  "${DESC}" \
  "" \
  "${NOTES}"

echo "beam.codex_account_reset task created for dyad ${DYAD}"
