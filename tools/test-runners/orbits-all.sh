#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
BIN="${ROOT}/.artifacts/cargo-target/release/orbits-test-runner"
if [[ -x "${BIN}" ]]; then
  exec "${BIN}" all "$@"
fi
exec cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-tools/Cargo.toml" --bin orbits-test-runner -- all "$@"
