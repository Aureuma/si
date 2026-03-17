#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "${ROOT}"
exec cargo run -q -p si-rs-cli -- build homebrew render-tap-formula "$@"
