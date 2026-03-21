#!/usr/bin/env bash
set -euo pipefail

si_artifact_is_fresh() {
  local bin="$1"
  shift
  [[ -x "${bin}" ]] || return 1
  if find "$@" -type f -newer "${bin}" -print -quit 2>/dev/null | grep -q .; then
    return 1
  fi
  return 0
}
