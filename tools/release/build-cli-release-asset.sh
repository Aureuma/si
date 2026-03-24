#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
BIN="${ROOT}/.artifacts/cargo-target/release/si-rs"
if [[ -x "${BIN}" ]]; then
  exec "${BIN}" build self asset "$@"
fi
exec cargo run --locked --release -q -p si-rs-cli -- build self asset "$@"
