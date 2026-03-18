#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
BIN="${ROOT}/.artifacts/cargo-target/release/si-rs"
if [[ -x "${BIN}" ]]; then
  exec "${BIN}" build installer smoke-docker "$@"
fi
exec cargo run -q -p si-rs-cli -- build installer smoke-docker "$@"
