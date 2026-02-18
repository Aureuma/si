#!/usr/bin/env bash
set -euo pipefail

repo_root() {
  local root
  root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
  printf '%s\n' "$root"
}

ensure_repo_root() {
  local root
  root="$(repo_root)"
  if [[ ! -f "${root}/go.work" ]]; then
    echo "go.work not found under ${root}; expected SI repo root layout" >&2
    exit 1
  fi
}

ensure_go() {
  if ! command -v go >/dev/null 2>&1; then
    echo "go is required but was not found on PATH" >&2
    exit 1
  fi
}

run_go_test() {
  local root
  root="$(repo_root)"
  (
    cd "${root}"
    go test "$@"
  )
}
