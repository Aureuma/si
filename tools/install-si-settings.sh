#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"
. "${ROOT}/tools/lib/artifact-fresh.sh"
BIN="${ROOT}/.artifacts/cargo-target/release/si-rs"
if si_artifact_is_fresh "${BIN}" \
  "${ROOT}/Cargo.toml" \
  "${ROOT}/Cargo.lock" \
  "${ROOT}/rust" \
  "${ROOT}/tools/install-si-settings.sh" \
  "${ROOT}/tools/lib/artifact-fresh.sh"; then
  exec "${BIN}" build installer settings "$@"
fi
exec cargo run -q -p si-rs-cli -- build installer settings "$@"
