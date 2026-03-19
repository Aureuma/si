#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT}"
BIN="${ROOT}/.artifacts/cargo-target/release/import-dotenv-to-si-vault"
if [[ -x "${BIN}" ]]; then
  exec "${BIN}" "$@"
fi
exec cargo run --quiet --locked --manifest-path "${ROOT}/rust/crates/si-tools/Cargo.toml" --bin import-dotenv-to-si-vault -- "$@"
