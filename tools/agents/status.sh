#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

source tools/agents/config.sh

print_latest() {
  local agent="$1"
  local dir="${AGENT_LOG_ROOT}/${agent}"
  if [[ ! -d "${dir}" ]]; then
    echo "${agent}: no runs"
    return 0
  fi

  local latest
  latest="$(find "${dir}" -mindepth 1 -maxdepth 1 -type d | sort | tail -n 1)"
  if [[ -z "${latest}" ]]; then
    echo "${agent}: no runs"
    return 0
  fi

  echo "${agent}: ${latest}"
  if [[ -f "${latest}/summary.md" ]]; then
    sed -n '1,20p' "${latest}/summary.md"
  fi
  echo
}

print_latest "pr-guardian"
print_latest "website-sentry"
