#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

source tools/agents/config.sh
source tools/agents/lib.sh

agent_init "website-sentry"
trap 'release_lock' EXIT

if ! acquire_lock "website-sentry"; then
  append_summary "## Lock"
  append_summary "- SKIP: another website-sentry run is active"
  finalize_agent "skipped_locked"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "status=skipped_locked"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  exit 0
fi

append_summary "## Preconditions"
if ! require_cmd go git bash; then
  append_summary "- FAIL: required commands missing"
  finalize_agent "failed"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "status=failed"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  exit 1
fi
append_summary "- PASS: go/git/bash available"

append_summary ""
append_summary "## Health Checks"

check_cmd() {
  local label="$1"
  shift
  if run_with_retry "${WEBSITE_SENTRY_RETRY_ATTEMPTS}" "${WEBSITE_SENTRY_RETRY_DELAY_SECONDS}" "${label}" "$@"; then
    append_summary "- PASS: ${label}"
    return 0
  fi
  append_summary "- FAIL: ${label}"
  return 1
}

run_health_suite() {
  local healthy=true
  check_cmd "go test tools/si" go test ./tools/si/... || healthy=false
  check_cmd "workspace tests" ./tools/test.sh || healthy=false
  if have_cmd docker; then
    check_cmd "installer docker smoke" ./tools/test-install-si-docker.sh || healthy=false
  else
    append_summary "- WARN: docker not available; skipped installer docker smoke"
  fi
  [[ "${healthy}" == "true" ]]
}

if run_health_suite; then
  append_summary ""
  append_summary "No remediation required."
  status="healthy"
  finalize_agent "${status}"
  if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
    {
      echo "status=${status}"
      echo "summary_file=${AGENT_SUMMARY_FILE}"
      echo "run_dir=${AGENT_RUN_DIR}"
    } >> "${GITHUB_OUTPUT}"
  fi
  log_info "website-sentry completed without remediation"
  exit 0
fi

append_summary ""
append_summary "## Remediation"
append_summary "- strategy: format/lint repair, then re-run health suite"

status="failed"
attempt=1
while [[ "${attempt}" -le "${WEBSITE_SENTRY_MAX_REMEDIATION_ATTEMPTS}" ]]; do
  append_summary "- remediation attempt ${attempt}/${WEBSITE_SENTRY_MAX_REMEDIATION_ATTEMPTS}"
  mapfile -t GO_FILES < <(git ls-files '*.go')
  if [[ "${#GO_FILES[@]}" -gt 0 ]]; then
    run_logged "gofmt repo go files" gofmt -w "${GO_FILES[@]}" || true
  fi
  if have_cmd shfmt; then
    run_logged "shfmt agent scripts" shfmt -w tools/agents/*.sh || true
  fi

  if run_health_suite; then
    if git diff --quiet; then
      status="recovered_without_changes"
      append_summary "- PASS: recovered without source changes"
    else
      status="remediated_with_changes"
      append_summary "- PASS: recovered with source changes"
    fi
    break
  fi

  attempt=$((attempt + 1))
done

if [[ "${status}" == "failed" ]]; then
  append_summary "- FAIL: remediation exhausted"
fi

if [[ -n "${GITHUB_OUTPUT:-}" ]]; then
  {
    echo "status=${status}"
    echo "summary_file=${AGENT_SUMMARY_FILE}"
    echo "run_dir=${AGENT_RUN_DIR}"
  } >> "${GITHUB_OUTPUT}"
fi

finalize_agent "${status}"
if [[ "${status}" == "failed" ]]; then
  log_error "website-sentry failed after remediation attempts"
  exit 1
fi

log_info "website-sentry completed (status=${status})"
