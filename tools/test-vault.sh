#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: ./tools/test-vault.sh [--quick]

Runs strict vault-focused tests.

Modes:
  --quick   Skip e2e subprocess vault tests.

Environment:
  SI_GO_TEST_TIMEOUT   go test timeout (default: 20m)
USAGE
  exit 0
fi

quick_mode=0
if [[ "${1:-}" == "--quick" ]]; then
  quick_mode=1
  shift
fi

if [[ "$#" -gt 0 ]]; then
  echo "unexpected arguments: $*" >&2
  echo "Run ./tools/test-vault.sh --help for usage." >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go is required but was not found on PATH" >&2
  echo "Install Go and try again. See docs/testing.md for details." >&2
  exit 1
fi

if [[ ! -f go.work ]]; then
  echo "go.work not found. Run this script from the repo root." >&2
  exit 1
fi

go_test_timeout="${SI_GO_TEST_TIMEOUT:-20m}"
echo "go version: $(go version)"
echo "go test timeout: ${go_test_timeout}"

echo "[1/3] vault command wiring + guardrail unit tests"
go test -timeout "${go_test_timeout}" -count=1 -shuffle=on \
  -run '^(TestVaultCommandActionSetsArePopulated|TestVaultActionNamesMatchDispatchSwitches|TestVaultValidateImplicitTargetRepoScope.*)$' \
  ./tools/si

echo "[2/3] vault internal package tests"
go test -timeout "${go_test_timeout}" -count=1 -shuffle=on ./tools/si/internal/vault/...

if [[ "${quick_mode}" -eq 0 ]]; then
  echo "[3/3] vault e2e subprocess tests"
  go test -timeout "${go_test_timeout}" -count=1 -shuffle=on -run '^TestVaultE2E_' ./tools/si
else
  echo "[3/3] skipped vault e2e subprocess tests (--quick)"
fi

echo "vault strict test suite: ok"
