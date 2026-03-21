#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
. "${ROOT}/tools/lib/artifact-fresh.sh"
BIN="${ROOT}/.artifacts/cargo-target/release/si-rs"
if si_artifact_is_fresh "${BIN}" \
  "${ROOT}/Cargo.toml" \
  "${ROOT}/Cargo.lock" \
  "${ROOT}/rust" \
  "${ROOT}/tools/release/build-cli-release-assets.sh" \
  "${ROOT}/tools/lib/artifact-fresh.sh"; then
  exec "${BIN}" build self release-assets "$@"
fi
exec cargo run --locked --release -q -p si-rs-cli -- build self release-assets "$@"
