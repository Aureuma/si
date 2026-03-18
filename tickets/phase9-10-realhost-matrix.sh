#!/usr/bin/env bash
set -euo pipefail

ROOT=${ROOT:-"$(cd "$(dirname "$0")/.." && pwd)"}
: "${SI_RS:=./.artifacts/cargo-target/release/si-rs}"
: "${SI_BIN:=cargo run --locked --package si-rs-cli --quiet --}"
: "${TIMEOUT_SECS:=600}"
: "${RELEASE_VERSION:=v0.54.0}"
: "${RELEASE_VERSION_NO_V:=}"
: "${RELEASE_ASSET_TIMEOUT_SECS:=3600}"
: "${SKIP_RELEASE_BUILD:=auto}"
: "${SMOKE_TIMEOUT_SECS:=120}"
: "${OUT_DIR:=/tmp/si-e2e/releases/single}"
: "${MULTI_DIR:=/tmp/si-e2e/releases/multi}"
: "${NPM_OUT:=/tmp/si-e2e/npm}"
: "${HOME_FORMULA:=/tmp/si-e2e/homebrew/core.rb}"
: "${TAP_FORMULA:=/tmp/si-e2e/homebrew/tap.rb}"
: "${LOG_FILE:=tickets/phase9-10-realhost-matrix-latest.log}"
export LOG_FILE
export SI_INSTALLER_USE_PREBUILT=1
exec > >(tee -a "${LOG_FILE}")
exec 2>&1

if [[ -x "${SI_RS}" ]]; then
  SI_CMD=("${SI_RS}")
else
  read -r -a SI_CMD <<< "${SI_BIN}"
fi
RELEASE_VERSION_NO_V="${RELEASE_VERSION_NO_V:-${RELEASE_VERSION#v}}"

run() {
  local label="$1"
  shift
  local status=0
  echo "== ${label} =="
  if [[ -n "${TIMEOUT_SECS}" && "${TIMEOUT_SECS}" != "0" ]]; then
    timeout "${TIMEOUT_SECS}" "$@"
    status=$?
  else
    "$@"
    status=$?
  fi
  if [[ ${status} -eq 124 ]]; then
    echo "WARN: ${label} timed out after ${TIMEOUT_SECS}s"
    return ${status}
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "WARN: ${label} failed with exit ${status}"
    return ${status}
  fi
  return 0
}

run_release() {
  local label="$1"
  shift
  run "${label}" "${SI_CMD[@]}" "$@"
}

run_si() {
  local label="$1"
  shift
  run_release "${label}" "$@"
}

run_si_with_env() {
  local label="$1"
  shift
  local env_kv="$1"
  shift
  run "${label}" env "${env_kv}" "${SI_CMD[@]}" "$@"
}

run_smoke_with_env() {
  local label="$1"
  shift
  local env_kv="$1"
  shift
  local status=0
  echo "== ${label} =="
  if [[ -n "${SMOKE_TIMEOUT_SECS}" && "${SMOKE_TIMEOUT_SECS}" != "0" ]]; then
    timeout "${SMOKE_TIMEOUT_SECS}" env "${env_kv}" "${SI_CMD[@]}" "$@"
    status=$?
  else
    env "${env_kv}" "${SI_CMD[@]}" "$@"
    status=$?
  fi
  if [[ ${status} -eq 124 ]]; then
    echo "WARN: ${label} timed out after ${SMOKE_TIMEOUT_SECS}s"
    return ${status}
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "WARN: ${label} failed with exit ${status}"
    return ${status}
  fi
  return 0
}

run_release_asset() {
  local label="$1"
  shift
  run_with_timeout "${label}" "${RELEASE_ASSET_TIMEOUT_SECS}" "${SI_CMD[@]}" "$@"
}

run_with_timeout() {
  local label="$1"
  shift
  local timeout_secs="$1"
  shift
  echo "== ${label} =="
  local status=0
  if [[ -n "${timeout_secs}" && "${timeout_secs}" != "0" ]]; then
    timeout "${timeout_secs}" "$@"
    status=$?
  else
    "$@"
    status=$?
  fi
  if [[ ${status} -eq 124 ]]; then
    echo "WARN: ${label} timed out after ${timeout_secs}s"
    return ${status}
  fi
  if [[ ${status} -ne 0 ]]; then
    echo "WARN: ${label} failed with exit ${status}"
    return ${status}
  fi
  return 0
}

echo "Root: ${ROOT}"
echo "Log: ${LOG_FILE}"
echo "Release asset timeout: ${RELEASE_ASSET_TIMEOUT_SECS}s"
echo "Smoke timeout: ${SMOKE_TIMEOUT_SECS}s"
cd "${ROOT}"

run_si "version" version
run_si "validate-release-version ok" build self validate-release-version --tag "${RELEASE_VERSION}"
run_si "validate-release-version error" build self validate-release-version --tag "${RELEASE_VERSION#v}" || true
single_stem="si_${RELEASE_VERSION_NO_V}_linux_amd64"
if [[ "${SKIP_RELEASE_BUILD}" == "1" || -f "${OUT_DIR}/${single_stem}.tar.gz" ]]; then
  echo "SKIP: single release-asset already present at ${OUT_DIR}/${single_stem}.tar.gz"
else
  if ! run_release_asset "release-asset" build self release-asset --version "${RELEASE_VERSION}" --goos linux --goarch amd64 --out-dir "${OUT_DIR}"; then
    run_release_asset "release-asset (fallback semver)" build self release-asset --version "${RELEASE_VERSION_NO_V}" --goos linux --goarch amd64 --out-dir "${OUT_DIR}"
  fi
fi
if [[ "${SKIP_RELEASE_BUILD}" == "1" || -f "${MULTI_DIR}/checksums.txt" ]]; then
  echo "SKIP: release-assets already present at ${MULTI_DIR}"
else
  if ! run_release_asset "release-assets" build self release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}"; then
    echo "WARN: release-assets failed or timed out"
  fi
fi
if [[ -f "${MULTI_DIR}/checksums.txt" ]]; then
  if ! run_si "verify-release-assets" build self verify-release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}"; then
    echo "WARN: checksum verification failed; attempting fresh release-assets generation"
    if [[ "${SKIP_RELEASE_BUILD}" != "1" ]]; then
      if ! run_release_asset "release-assets (retry)" build self release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}"; then
        echo "WARN: release-assets retry failed or timed out; continuing matrix for remaining lanes"
      elif ! run_si "verify-release-assets (retry)" build self verify-release-assets --version "${RELEASE_VERSION}" --out-dir "${MULTI_DIR}"; then
        echo "WARN: verify-release-assets still failing after retry; continuing matrix for remaining lanes"
      fi
    else
      echo "WARN: SKIP_RELEASE_BUILD=1, not rebuilding multi-asset set"
    fi
  fi
else
  echo "WARN: release-assets output missing, skipping verification"
fi
run_si "homebrew core formula" build homebrew render-core-formula --version "${RELEASE_VERSION}" --output "${HOME_FORMULA}"
if [[ -f "${MULTI_DIR}/checksums.txt" ]]; then
  run_si "homebrew tap formula" build homebrew render-tap-formula --version "${RELEASE_VERSION}" --checksums "${MULTI_DIR}/checksums.txt" --output "${TAP_FORMULA}"
  run_si "homebrew update tap" build homebrew update-tap-repo --version "${RELEASE_VERSION}" --checksums "${MULTI_DIR}/checksums.txt" --tap-dir /tmp/si-e2e/tap
else
  echo "WARN: checksums missing, skipping tap formula/update commands"
fi
run_si "npm build package" build npm build-package --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --out-dir "${NPM_OUT}"
run_si "npm publish package dry-run" build npm publish-package --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --dry-run
run_si "npm publish from vault dry-run" build npm publish-from-vault --repo-root "${ROOT}" --version "${RELEASE_VERSION}" --dry-run || true
tmp_settings=$(mktemp /tmp/si-e2e-settings-XXXX.toml)
run_si "installer settings helper print" build installer settings-helper --settings "${tmp_settings}" --default-browser safari --print || true
rm -f "${tmp_settings}"
run_smoke_with_env "installer smoke-host" SI_INSTALL_SMOKE_SKIP_NONROOT=1 build installer smoke-host || true
if command -v npm >/dev/null 2>&1; then
  run_smoke_with_env "installer smoke-npm" SI_INSTALL_SMOKE_SKIP_NONROOT=1 build installer smoke-npm || true
else
  echo "SKIP: npm command not available; skipping npm installer smoke"
fi
if command -v docker >/dev/null 2>&1; then
  run_smoke_with_env "installer smoke-docker" SI_INSTALL_SMOKE_SKIP_NONROOT=1 build installer smoke-docker || true
else
  echo "SKIP: docker command not available; skipping docker installer smoke"
fi
run_si "installer smoke-homebrew" build installer smoke-homebrew || true

echo "Matrix complete."
