#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

echo "== Agent doctor =="

required=(bash git python3 go)
optional=(shfmt gofmt)

for cmd in "${required[@]}"; do
  if command -v "${cmd}" >/dev/null 2>&1; then
    echo "PASS required command: ${cmd}"
  else
    echo "FAIL required command missing: ${cmd}"
    exit 1
  fi
done

for cmd in "${optional[@]}"; do
  if command -v "${cmd}" >/dev/null 2>&1; then
    echo "PASS optional command: ${cmd}"
  else
    echo "WARN optional command missing: ${cmd}"
  fi
done

echo
echo "== Syntax checks =="
bash -n tools/agents/config.sh
bash -n tools/agents/lib.sh
bash -n tools/agents/pr-guardian.sh
bash -n tools/agents/website-sentry.sh
bash -n tools/agents/market-research-scout.sh
bash -n tools/agents/status.sh

echo "PASS shell syntax"
