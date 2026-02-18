#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

source tools/agents/config.sh
source tools/agents/lib.sh

agent_init "market-research-scout"
trap 'release_lock' EXIT

if ! acquire_lock "market-research-scout"; then
  append_summary "## Lock"
  append_summary "- SKIP: another market-research-scout run is active"
  finalize_agent "skipped_locked"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "status=skipped_locked"
      echo "new_tasks=0"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

append_summary "## Preconditions"
if ! require_cmd python3 git; then
  append_summary "- FAIL: python3/git missing"
  finalize_agent "failed"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "status=failed"
      echo "new_tasks=0"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  exit 1
fi
append_summary "- PASS: python3/git available"
append_summary ""

MAX_OPPS="${MARKET_RESEARCH_MAX_OPPORTUNITIES:-6}"
MAX_NEW="${MARKET_RESEARCH_MAX_NEW_TICKETS:-3}"
FEEDS="${MARKET_RESEARCH_FEEDS:-}"
SUMMARY_JSON="${AGENT_RUN_DIR}/summary.json"

cmd=(
  python3 tools/agents/market_research_scout.py
  --repo-root "${ROOT_DIR}"
  --board-json "tickets/taskboard/shared-taskboard.json"
  --board-md "tickets/taskboard/SHARED_TASKBOARD.md"
  --report-dir "docs/market-research/opportunities"
  --tickets-dir "tickets/market-research"
  --summary-json "${SUMMARY_JSON}"
  --max-opportunities "${MAX_OPPS}"
  --max-new-tickets "${MAX_NEW}"
)
if [[ -n "${FEEDS}" ]]; then
  cmd+=(--feeds "${FEEDS}")
fi

run_logged "market intelligence scan" "${cmd[@]}"

read_json_field() {
  local key="$1"
  python3 - "$SUMMARY_JSON" "$key" <<'PY'
import json, sys
path, key = sys.argv[1], sys.argv[2]
with open(path, 'r', encoding='utf-8') as f:
    data = json.load(f)
value = data.get(key)
if isinstance(value, bool):
    print(str(value).lower())
elif value is None:
    print('')
else:
    print(value)
PY
}

status="ok"
signals_scanned="$(read_json_field signals_scanned)"
scored_opportunities="$(read_json_field scored_opportunities)"
top_opportunities="$(read_json_field top_opportunities)"
new_tasks="$(read_json_field new_tasks)"
report_path="$(read_json_field report_path)"

append_summary "## Run Results"
append_summary "- signals_scanned: ${signals_scanned}"
append_summary "- scored_opportunities: ${scored_opportunities}"
append_summary "- top_opportunities: ${top_opportunities}"
append_summary "- new_tasks: ${new_tasks}"
append_summary "- report: \`${report_path}\`"

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "status=${status}"
    echo "new_tasks=${new_tasks}"
    echo "report_path=${report_path}"
    echo "summary_file=${AGENT_SUMMARY_FILE}"
    echo "run_dir=${AGENT_RUN_DIR}"
  } >> "${GITHUB_OUTPUT}"
fi

finalize_agent "${status}"
log_info "market-research-scout completed"
