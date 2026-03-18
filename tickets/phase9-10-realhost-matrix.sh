#!/usr/bin/env bash
set -euo pipefail

ROOT=${ROOT:-"$(cd "$(dirname "$0")/.." && pwd)"}
: "${SI_RS:=./.artifacts/cargo-target/release/si-rs}"
: "${SI_BIN:=cargo run --locked --package si-rs-cli --quiet --}"
: "${TIMEOUT_SECS:=600}"
: "${RELEASE_VERSION:=v0.54.0}"
: "${OUT_DIR:=/tmp/si-e2e/releases/single}"
: "${MULTI_DIR:=/tmp/si-e2e/releases/multi}"
: "${NPM_OUT:=/tmp/si-e2e/npm}"
: "${HOME_FORMULA:=/tmp/si-e2e/homebrew/core.rb}"
: "${TAP_FORMULA:=/tmp/si-e2e/homebrew/tap.rb}"

run() {
  local label="$1"
  shift
  echo "== $label =="
  if [[ -n "${TIMEOUT_SECS}" ]]; then
    timeout "${TIMEOUT_SECS}" "$@"
  else
    "$@"
  fi
}

si_cmd() {
  if [[ -x "${SI_RS}" ]]; then
    "${SI_RS}" "$@"
  else
    eval "${SI_BIN}" "$@"
  fi
}

echo "Root: ${ROOT}"
cd "${ROOT}"

run "version" si_cmd version
run "validate-release-version ok" si_cmd build self validate-release-version --tag "${RELEASE_VERSION}"
run "validate-release-version error" si_cmd build self validate-release-version --tag "${RELEASE_VERSION#v}"
run "release-asset" si_cmd build self release-asset --version "${RELEASE_VERSION}" --goos linux --goarch amd64 --out-dir "${OUT_DIR}"
run "release-assets" si_cmd build self release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}" || true
run "verify-release-assets" si_cmd build self verify-release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}" || true
run "homebrew core formula" si_cmd build homebrew render-core-formula --version "${RELEASE_VERSION}" --output "${HOME_FORMULA}"
run "homebrew tap formula" si_cmd build homebrew render-tap-formula --version "${RELEASE_VERSION}" --checksums "${MULTI_DIR}/checksums.txt" --output "${TAP_FORMULA}" || true
run "homebrew update tap" si_cmd build homebrew update-tap-repo --version "${RELEASE_VERSION}" --checksums "${MULTI_DIR}/checksums.txt" --tap-dir /tmp/si-e2e/tap || true
run "npm build package" si_cmd build npm build-package --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --out-dir "${NPM_OUT}"
run "npm publish package dry-run" si_cmd build npm publish-package --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --dry-run
run "npm publish from vault dry-run" si_cmd build npm publish-from-vault --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --dry-run || true
run "installer settings helper print" si_cmd build installer settings-helper --print
run "installer smoke-host" SI_INSTALL_SMOKE_SKIP_NONROOT=1 si_cmd build installer smoke-host
run "installer smoke-npm" SI_INSTALL_SMOKE_SKIP_NONROOT=1 si_cmd build installer smoke-npm
run "installer smoke-docker" SI_INSTALL_SMOKE_SKIP_NONROOT=1 si_cmd build installer smoke-docker || true
run "installer smoke-homebrew" si_cmd build installer smoke-homebrew || true

echo "Matrix complete."
