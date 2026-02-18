#!/usr/bin/env bash
set -euo pipefail

AGENT_LOG_ROOT="${AGENT_LOG_ROOT:-.artifacts/agent-logs}"
AGENT_LOCK_ROOT="${AGENT_LOCK_ROOT:-.tmp/agent-locks}"
AGENT_LOG_RETENTION_DAYS="${AGENT_LOG_RETENTION_DAYS:-14}"

timestamp_utc() {
  date -u +"%Y-%m-%dT%H:%M:%SZ"
}

timestamp_unix() {
  date -u +%s
}

agent_init() {
  if [[ $# -lt 1 ]]; then
    echo "usage: agent_init <agent-name>" >&2
    return 1
  fi
  AGENT_NAME="$1"
  AGENT_RUN_ID="$(date -u +%Y%m%dT%H%M%SZ)-$$"
  AGENT_RUN_DIR="${AGENT_LOG_ROOT}/${AGENT_NAME}/${AGENT_RUN_ID}"
  AGENT_LOG_FILE="${AGENT_RUN_DIR}/run.log"
  AGENT_LOG_JSON_FILE="${AGENT_RUN_DIR}/run.jsonl"
  AGENT_SUMMARY_FILE="${AGENT_RUN_DIR}/summary.md"
  AGENT_META_FILE="${AGENT_RUN_DIR}/meta.json"
  AGENT_LOCK_DIR=""

  mkdir -p "${AGENT_RUN_DIR}"
  : > "${AGENT_LOG_FILE}"
  : > "${AGENT_LOG_JSON_FILE}"
  {
    echo "# ${AGENT_NAME} run ${AGENT_RUN_ID}"
    echo
    echo "- started: $(timestamp_utc)"
    echo "- run_dir: \`${AGENT_RUN_DIR}\`"
    echo
  } > "${AGENT_SUMMARY_FILE}"
  {
    echo "{"
    echo "  \"agent\": \"${AGENT_NAME}\","
    echo "  \"run_id\": \"${AGENT_RUN_ID}\","
    echo "  \"started_at\": \"$(timestamp_utc)\","
    echo "  \"pid\": $$"
    echo "}"
  } > "${AGENT_META_FILE}"

  export AGENT_NAME AGENT_RUN_ID AGENT_RUN_DIR AGENT_LOG_FILE AGENT_LOG_JSON_FILE
  export AGENT_SUMMARY_FILE AGENT_META_FILE AGENT_LOCK_DIR
  log_info "initialized ${AGENT_NAME} (run=${AGENT_RUN_ID})"
  cleanup_old_runs
}

log_line() {
  local level="$1"
  shift
  local msg="$*"
  local ts
  ts="$(timestamp_utc)"
  printf "%s [%s] %s\n" "${ts}" "${level}" "${msg}" | tee -a "${AGENT_LOG_FILE}"
  printf '{"ts":"%s","level":"%s","msg":"%s"}\n' \
    "${ts}" \
    "${level}" \
    "$(printf '%s' "${msg}" | sed 's/\\/\\\\/g; s/"/\\"/g')" \
    >> "${AGENT_LOG_JSON_FILE}"
}

log_info() {
  log_line "INFO" "$*"
}

log_warn() {
  log_line "WARN" "$*"
}

log_error() {
  log_line "ERROR" "$*"
}

append_summary() {
  printf "%s\n" "$*" >> "${AGENT_SUMMARY_FILE}"
}

run_logged() {
  if [[ $# -lt 2 ]]; then
    echo "usage: run_logged <label> <command...>" >&2
    return 1
  fi
  local label="$1"
  shift

  local start end rc
  start="$(date +%s)"
  log_info "BEGIN ${label} :: $*"
  set +e
  "$@" >> "${AGENT_LOG_FILE}" 2>&1
  rc=$?
  set -e
  end="$(date +%s)"

  if [[ ${rc} -eq 0 ]]; then
    log_info "END ${label} :: rc=${rc} duration=$((end - start))s"
  else
    log_error "END ${label} :: rc=${rc} duration=$((end - start))s"
  fi
  return "${rc}"
}

run_with_retry() {
  if [[ $# -lt 3 ]]; then
    echo "usage: run_with_retry <attempts> <delay-seconds> <label> <command...>" >&2
    return 1
  fi

  local attempts="$1"
  local delay="$2"
  local label="$3"
  shift 3

  local i rc
  for ((i = 1; i <= attempts; i++)); do
    if run_logged "${label} (attempt ${i}/${attempts})" "$@"; then
      return 0
    fi
    rc=$?
    if [[ "${i}" -lt "${attempts}" ]]; then
      log_warn "${label} failed on attempt ${i}, retrying in ${delay}s"
      sleep "${delay}"
      delay=$((delay * 2))
    fi
  done
  return "${rc:-1}"
}

git_changed_files() {
  git diff --name-only
}

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

require_cmd() {
  local missing=()
  local cmd
  for cmd in "$@"; do
    if ! have_cmd "${cmd}"; then
      missing+=("${cmd}")
    fi
  done
  if [[ "${#missing[@]}" -eq 0 ]]; then
    return 0
  fi
  log_error "missing required commands: ${missing[*]}"
  return 1
}

acquire_lock() {
  if [[ $# -lt 1 ]]; then
    echo "usage: acquire_lock <lock-key>" >&2
    return 1
  fi
  local key="$1"
  mkdir -p "${AGENT_LOCK_ROOT}"
  AGENT_LOCK_DIR="${AGENT_LOCK_ROOT}/${key}.lock"
  if mkdir "${AGENT_LOCK_DIR}" 2>/dev/null; then
    printf '%s\n' "$$" > "${AGENT_LOCK_DIR}/pid"
    log_info "acquired lock ${AGENT_LOCK_DIR}"
    return 0
  fi
  log_warn "lock busy ${AGENT_LOCK_DIR}; another run is in progress"
  return 1
}

release_lock() {
  if [[ -n "${AGENT_LOCK_DIR:-}" && -d "${AGENT_LOCK_DIR}" ]]; then
    rm -rf "${AGENT_LOCK_DIR}"
    log_info "released lock ${AGENT_LOCK_DIR}"
  fi
}

finalize_agent() {
  local status="${1:-unknown}"
  append_summary ""
  append_summary "## Run Metadata"
  append_summary "- completed: $(timestamp_utc)"
  append_summary "- status: \`${status}\`"
  append_summary "- run log: \`${AGENT_LOG_FILE}\`"
  append_summary "- json log: \`${AGENT_LOG_JSON_FILE}\`"
}

cleanup_old_runs() {
  if [[ ! -d "${AGENT_LOG_ROOT}" ]]; then
    return 0
  fi
  find "${AGENT_LOG_ROOT}" -type d -mindepth 3 -maxdepth 3 \
    -mtime "+${AGENT_LOG_RETENTION_DAYS}" -exec rm -rf {} + >/dev/null 2>&1 || true
}
