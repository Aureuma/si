#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
RUST_BIN="${ROOT}/.artifacts/cargo-target/release/agents-status"
if [[ -x "${RUST_BIN}" ]]; then
  exec "${RUST_BIN}" "$@"
fi
exec cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-agents/Cargo.toml" --bin agents-status -- "$@"
