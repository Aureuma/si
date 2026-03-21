#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
. "${ROOT}/tools/lib/artifact-fresh.sh"
RUST_BIN="${ROOT}/.artifacts/cargo-target/release/agents-doctor"
if si_artifact_is_fresh "${RUST_BIN}" \
  "${ROOT}/Cargo.toml" \
  "${ROOT}/Cargo.lock" \
  "${ROOT}/rust" \
  "${ROOT}/tools/agents/doctor.sh" \
  "${ROOT}/tools/lib/artifact-fresh.sh"; then
  exec "${RUST_BIN}" "$@"
fi
exec cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-agents/Cargo.toml" --bin agents-doctor -- "$@"
