#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
. "${ROOT}/tools/lib/artifact-fresh.sh"
BIN="${ROOT}/.artifacts/cargo-target/release/fake-codex"
if si_artifact_is_fresh "${BIN}" \
  "${ROOT}/Cargo.toml" \
  "${ROOT}/Cargo.lock" \
  "${ROOT}/rust" \
  "${ROOT}/tools/dyad/fake-codex.sh" \
  "${ROOT}/tools/lib/artifact-fresh.sh"; then
  exec "${BIN}" "$@"
fi
exec cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-tools/Cargo.toml" --bin fake-codex -- "$@"
