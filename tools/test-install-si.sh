#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT}/tools/install-si.sh"

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

note "e2e: idempotent reinstall over existing binary"
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${install_dir}" --force --quiet
"${install_dir}/si" version >/dev/null

note "e2e: uninstall"
"${INSTALLER}" --install-dir "${install_dir}" --uninstall --quiet
[[ ! -e "${install_dir}/si" ]] || fail "expected ${install_dir}/si to be removed"

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

note "ok"

