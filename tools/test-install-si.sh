#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT}/tools/install-si.sh"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'EOF'
Usage: ./tools/test-install-si.sh

Runs installer smoke tests against tools/install-si.sh.
EOF
  exit 0
fi

if [[ "$#" -gt 0 ]]; then
  echo "unexpected arguments: $*" >&2
  echo "Run ./tools/test-install-si.sh --help for usage." >&2
  exit 1
fi

if ! command -v git >/dev/null 2>&1; then
  echo "FAIL: git is required to run installer tests" >&2
  exit 1
fi

if [[ ! -f "${INSTALLER}" ]]; then
  echo "FAIL: installer not found at ${INSTALLER}" >&2
  exit 1
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

note() {
  echo "==> $*" >&2
}

tmp="$(mktemp -d)"
cleanup() {
  rm -rf "${tmp}" || true
}
trap cleanup EXIT

note "syntax check"
bash -n "${INSTALLER}"

note "help output"
"${INSTALLER}" --help >/dev/null

note "dry-run: linux/amd64 install-dir with spaces"
mkdir -p "${tmp}/bin dir"
"${INSTALLER}" --dry-run --source-dir "${ROOT}" --install-dir "${tmp}/bin dir" --force >/dev/null

note "dry-run: darwin/arm64 go download URL computation"
"${INSTALLER}" --dry-run --source-dir "${ROOT}" --os darwin --arch arm64 --go-mode auto --force >/dev/null

note "e2e: install from local checkout into temp bin"
install_dir="${tmp}/bin"
mkdir -p "${install_dir}"
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${install_dir}" --force --quiet

[[ -x "${install_dir}/si" ]] || fail "expected installed binary at ${install_dir}/si"
"${install_dir}/si" version >/dev/null
"${install_dir}/si" --help >/dev/null

note "e2e: install-path override (with spaces)"
install_path="${tmp}/bin custom/si"
mkdir -p "$(dirname "${install_path}")"
"${INSTALLER}" --source-dir "${ROOT}" --install-path "${install_path}" --force --quiet
[[ -x "${install_path}" ]] || fail "expected installed binary at ${install_path}"
"${install_path}" version >/dev/null

note "e2e: idempotent reinstall over existing binary"
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${install_dir}" --force --quiet
"${install_dir}/si" version >/dev/null

note "edge: reinstall without --force fails when binary exists"
set +e
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${install_dir}" --quiet >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected install to fail when binary exists and --force is not provided"
fi

note "e2e: uninstall"
"${INSTALLER}" --install-dir "${install_dir}" --uninstall --quiet
[[ ! -e "${install_dir}/si" ]] || fail "expected ${install_dir}/si to be removed"

note "edge: uninstall when not installed succeeds"
"${INSTALLER}" --install-dir "${install_dir}" --uninstall --quiet

note "edge: unwritable install dir"
ro="${tmp}/ro"
mkdir -p "${ro}"
chmod 000 "${ro}"
set +e
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${ro}" --force --quiet >/dev/null 2>&1
rc=$?
set -e
chmod 755 "${ro}"
if [[ $rc -eq 0 ]]; then
  fail "expected install to fail for unwritable dir"
fi

note "edge: go-mode system fails when go not in PATH"
set +e
PATH="/usr/bin:/bin" "${INSTALLER}" --dry-run --source-dir "${ROOT}" --go-mode system --force --quiet >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected --go-mode system to fail when go is not in PATH"
fi

note "edge: --os/--arch overrides are rejected without --dry-run"
set +e
"${INSTALLER}" --source-dir "${ROOT}" --os darwin --arch arm64 --force --quiet >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected --os/--arch overrides to fail without --dry-run"
fi

note "edge: clone+checkout by commit sha (local repo-url)"
sha="$(git -C "${ROOT}" rev-parse HEAD)"
clone_install_dir="${tmp}/bin-clone"
mkdir -p "${clone_install_dir}"
"${INSTALLER}" --repo-url "${ROOT}" --ref "${sha}" --install-dir "${clone_install_dir}" --force --quiet
[[ -x "${clone_install_dir}/si" ]] || fail "expected installed binary at ${clone_install_dir}/si"
"${clone_install_dir}/si" version >/dev/null

note "ok"
