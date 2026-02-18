#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'EOF'
Usage: ./tools/test.sh

Runs Go tests across workspace modules listed in go.work.
Use --list to print the module list without running tests.

Environment:
  SI_GO_TEST_TIMEOUT   go test timeout (default: 15m)
EOF
  exit 0
fi

if [[ "$#" -gt 1 ]]; then
  echo "unexpected arguments: $*" >&2
  echo "Run ./tools/test.sh --help for usage." >&2
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

echo "go version: $(go version)"
go_test_timeout="${SI_GO_TEST_TIMEOUT:-15m}"
echo "go test timeout: ${go_test_timeout}"

modules=(
  ./agents/critic/...
  ./agents/shared/...
  ./tools/codex-init/...
  ./tools/codex-interactive-driver/...
  ./tools/codex-stdout-parser/...
  ./tools/si/...
)

if [[ "${1:-}" == "--list" ]]; then
  printf '%s\n' "${modules[@]}"
  exit 0
fi

if [[ "$#" -eq 1 ]]; then
  echo "unknown option: ${1}" >&2
  echo "Run ./tools/test.sh --help for usage." >&2
  exit 1
fi

echo "Running go test on:" "${modules[@]}"
go test -timeout "${go_test_timeout}" "${modules[@]}"
