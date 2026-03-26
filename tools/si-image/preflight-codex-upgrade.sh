#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
. "${ROOT}/tools/lib/artifact-fresh.sh"
ARTIFACT_TARGET_DIR="${ROOT}/.artifacts/cargo-target"
BIN="${ARTIFACT_TARGET_DIR}/release/preflight-codex-upgrade"
if si_artifact_is_fresh "${BIN}" \
  "${ROOT}/Cargo.toml" \
  "${ROOT}/Cargo.lock" \
  "${ROOT}/rust" \
  "${ROOT}/tools/si-image/preflight-codex-upgrade.sh" \
  "${ROOT}/tools/lib/artifact-fresh.sh"; then
  exec "${BIN}" "$@"
fi

BUILD_TARGET_DIR="${CARGO_TARGET_DIR:-${ARTIFACT_TARGET_DIR}}"
if ! mkdir -p "${BUILD_TARGET_DIR}" 2>/dev/null || [[ ! -w "${BUILD_TARGET_DIR}" ]]; then
  BUILD_TARGET_DIR="$(mktemp -d "${TMPDIR:-/tmp}/si-preflight-cargo-target.XXXXXX")"
  trap 'rm -rf "${BUILD_TARGET_DIR}"' EXIT
fi

exec env CARGO_TARGET_DIR="${BUILD_TARGET_DIR}" cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-agents/Cargo.toml" --bin preflight-codex-upgrade -- "$@"
