#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

source tools/agents/config.sh
source tools/agents/lib.sh

agent_init "pr-guardian"
trap 'release_lock' EXIT

if ! require_cmd python3 git; then
  append_summary "## Preconditions"
  append_summary "- FAIL: missing required runtime commands"
  finalize_agent "failed"
  exit 1
fi

PR_NUMBER=""
BASE_REF="main"
HEAD_REF="${GITHUB_REF_NAME:-$(git rev-parse --abbrev-ref HEAD)}"
HEAD_REPO="${GITHUB_REPOSITORY:-}"
REPO="${GITHUB_REPOSITORY:-}"
EVENT_NAME="${GITHUB_EVENT_NAME:-manual}"
ALLOW_PUSH="${PR_GUARDIAN_ALLOW_PUSH:-false}"

if [[ -n "${GITHUB_EVENT_PATH:-}" && -f "${GITHUB_EVENT_PATH}" ]]; then
  PR_NUMBER="$(python3 - "${GITHUB_EVENT_PATH}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    event = json.load(f)
print(event.get("pull_request", {}).get("number", ""))
PY
)"

  BASE_REF="$(python3 - "${GITHUB_EVENT_PATH}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    event = json.load(f)
print(event.get("pull_request", {}).get("base", {}).get("ref", "main"))
PY
)"

  HEAD_REF="$(python3 - "${GITHUB_EVENT_PATH}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    event = json.load(f)
print(event.get("pull_request", {}).get("head", {}).get("ref", ""))
PY
)"

  HEAD_REPO="$(python3 - "${GITHUB_EVENT_PATH}" <<'PY'
import json, sys
with open(sys.argv[1], "r", encoding="utf-8") as f:
    event = json.load(f)
print(event.get("pull_request", {}).get("head", {}).get("repo", {}).get("full_name", ""))
PY
)"
fi

if [[ "${ALLOW_PUSH}" != "true" ]]; then
  if [[ "${EVENT_NAME}" == "pull_request" && -n "${PR_NUMBER}" ]]; then
    ALLOW_PUSH="true"
  fi
fi

lock_key="pr-guardian-${PR_NUMBER:-manual}"
if ! acquire_lock "${lock_key}"; then
  append_summary "## Lock"
  append_summary "- SKIP: another guardian run is active"
  finalize_agent "skipped_locked"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "risk=high"
      echo "changed_count=0"
      echo "autofix_applied=false"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

append_summary "## Preconditions"
append_summary "- PASS: git/python3 available"
append_summary "- base_ref: \`${BASE_REF}\`"
append_summary "- head_ref: \`${HEAD_REF:-unknown}\`"
append_summary "- event: \`${EVENT_NAME}\`"
append_summary "- push_enabled: \`${ALLOW_PUSH}\`"
append_summary ""

run_logged "fetch base branch" git fetch --no-tags --depth=1 origin "${BASE_REF}" || true

if git show-ref --quiet "refs/remotes/origin/${BASE_REF}"; then
  mapfile -t CHANGED_FILES < <(git diff --name-only "origin/${BASE_REF}"...HEAD)
else
  mapfile -t CHANGED_FILES < <(git diff --name-only HEAD~1...HEAD)
fi
CHANGED_COUNT="${#CHANGED_FILES[@]}"

risk="low"
if [[ "${CHANGED_COUNT}" -gt "${PR_GUARDIAN_RISK_HIGH_FILE_COUNT}" ]]; then
  risk="high"
elif [[ "${CHANGED_COUNT}" -gt "${PR_GUARDIAN_RISK_MEDIUM_FILE_COUNT}" ]]; then
  risk="medium"
fi

for file in "${CHANGED_FILES[@]}"; do
  case "${file}" in
    .github/workflows/*|tools/install-si.sh|tools/si/*paas*|agents/shared/docker/*)
      risk="high"
      ;;
    tools/*|docs/*|README.md)
      [[ "${risk}" == "low" ]] && risk="medium"
      ;;
  esac
done

append_summary "## Triage"
append_summary "- pr: #${PR_NUMBER:-manual}"
append_summary "- changed_files: ${CHANGED_COUNT}"
append_summary "- risk: \`${risk}\`"
append_summary ""

append_summary "## Safe Auto-Fix Actions"
git status --porcelain > "${AGENT_RUN_DIR}/status-before.txt"
declare -a SHFMT_FILES=()
declare -a GOFMT_FILES=()

for file in "${CHANGED_FILES[@]}"; do
  [[ -f "${file}" ]] || continue
  case "${file}" in
    *.sh)
      SHFMT_FILES+=("${file}")
      ;;
  esac
  case "${file}" in
    *.go)
      GOFMT_FILES+=("${file}")
      ;;
  esac
done

autofix_applied="false"
autofix_delta="false"
if [[ "${CHANGED_COUNT}" -le "${PR_GUARDIAN_MAX_AUTOFIX_FILES}" ]]; then
  if [[ "${#SHFMT_FILES[@]}" -gt 0 ]]; then
    if have_cmd shfmt; then
      run_logged "shfmt changed shell files" shfmt -w "${SHFMT_FILES[@]}" || true
    else
      append_summary "- WARN: shfmt unavailable, skipped shell auto-fix"
      log_warn "shfmt unavailable; skipping"
    fi
  fi

  if [[ "${#GOFMT_FILES[@]}" -gt 0 ]]; then
    if have_cmd gofmt; then
      run_logged "gofmt changed go files" gofmt -w "${GOFMT_FILES[@]}" || true
    else
      append_summary "- WARN: gofmt unavailable, skipped Go auto-fix"
      log_warn "gofmt unavailable; skipping"
    fi
  fi
else
  append_summary "- SKIP: too many files for safe auto-fix (${CHANGED_COUNT})"
fi

git status --porcelain > "${AGENT_RUN_DIR}/status-after.txt"
if ! cmp -s "${AGENT_RUN_DIR}/status-before.txt" "${AGENT_RUN_DIR}/status-after.txt"; then
  autofix_delta="true"
fi

if [[ "${autofix_delta}" == "true" ]]; then
  autofix_applied="true"
  if [[ "${ALLOW_PUSH}" == "true" && "${HEAD_REPO}" == "${REPO}" && -n "${HEAD_REF}" ]]; then
    git config user.name "si-agent[bot]"
    git config user.email "si-agent@users.noreply.github.com"
    git add -A
    run_logged "commit autofix changes" git commit -m "chore(agent): safe autofix for PR #${PR_NUMBER:-manual}" || true
    run_logged "push autofix branch updates" git push origin "HEAD:${HEAD_REF}" || true
    append_summary "- PASS: autofix applied and push attempted to \`${HEAD_REF}\`"
  else
    append_summary "- SKIP: autofix detected but push disabled (manual/fork/non-PR context)"
  fi
else
  append_summary "- PASS: no autofix changes required"
fi

append_summary ""
append_summary "## Changed Files"
if [[ "${CHANGED_COUNT}" -eq 0 ]]; then
  append_summary "- none"
else
  for file in "${CHANGED_FILES[@]}"; do
    append_summary "- \`${file}\`"
  done
fi

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "risk=${risk}"
    echo "changed_count=${CHANGED_COUNT}"
    echo "autofix_applied=${autofix_applied}"
    echo "summary_file=${AGENT_SUMMARY_FILE}"
    echo "run_dir=${AGENT_RUN_DIR}"
  } >> "${GITHUB_OUTPUT}"
fi

finalize_agent "completed"
log_info "pr-guardian completed"
