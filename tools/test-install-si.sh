#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALLER="${ROOT}/tools/install-si.sh"
SETTINGS_HELPER_TEST="${ROOT}/tools/test-install-si-settings.sh"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'EOF'
Usage: ./tools/test-install-si.sh

Runs installer smoke tests against tools/install-si.sh, including
settings helper regression tests.
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
if [[ ! -x "${SETTINGS_HELPER_TEST}" ]]; then
  echo "FAIL: installer settings helper test not found at ${SETTINGS_HELPER_TEST}" >&2
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

note "installer settings helper tests"
"${SETTINGS_HELPER_TEST}"

note "help output"
"${INSTALLER}" --help >/dev/null

note "dry-run: linux/amd64 install-dir with spaces"
mkdir -p "${tmp}/bin dir"
"${INSTALLER}" --dry-run --source-dir "${ROOT}" --install-dir "${tmp}/bin dir" --force >/dev/null

note "dry-run: darwin/arm64 go download URL computation"
"${INSTALLER}" --dry-run --source-dir "${ROOT}" --os darwin --arch arm64 --go-mode auto --force >/dev/null

note "dry-run: no-path-hint flag"
"${INSTALLER}" --dry-run --no-path-hint --source-dir "${ROOT}" --force >/dev/null

note "dry-run: --yes accepted"
"${INSTALLER}" --dry-run --yes --source-dir "${ROOT}" --force >/dev/null

note "dry-run: backend intent helia accepted"
"${INSTALLER}" --dry-run --backend helia --source-dir "${ROOT}" --force >/dev/null

note "edge: invalid backend rejected"
set +e
"${INSTALLER}" --dry-run --backend bad-backend --source-dir "${ROOT}" --force >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected installer to fail for invalid --backend"
fi

note "edge: install-dir and install-path are mutually exclusive"
set +e
"${INSTALLER}" --dry-run --source-dir "${ROOT}" --install-dir "${tmp}/x" --install-path "${tmp}/y/si" --force >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected installer to fail when --install-dir and --install-path are both provided"
fi

note "edge: invalid source-dir rejected"
set +e
"${INSTALLER}" --dry-run --source-dir "${tmp}/missing-source" --force >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected installer to fail for missing --source-dir"
fi

note "edge: non-si source-dir rejected"
mkdir -p "${tmp}/not-si"
set +e
"${INSTALLER}" --dry-run --source-dir "${tmp}/not-si" --force >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected installer to fail for --source-dir without tools/si/go.mod"
fi

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

note "e2e: install with explicit tmp-dir (path with spaces)"
tmp_dir="${tmp}/tmp build"
mkdir -p "${tmp_dir}"
"${INSTALLER}" --source-dir "${ROOT}" --tmp-dir "${tmp_dir}" --install-dir "${install_dir}" --force --quiet
"${install_dir}/si" version >/dev/null

note "e2e: non-quiet install works"
nonquiet_dir="${tmp}/bin-nonquiet"
mkdir -p "${nonquiet_dir}"
"${INSTALLER}" --source-dir "${ROOT}" --install-dir "${nonquiet_dir}" --force >/dev/null
"${nonquiet_dir}/si" version >/dev/null

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
if [[ "$(id -u)" -eq 0 ]]; then
  note "skip unwritable-dir assertion as root (root bypasses directory permission bits)"
else
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
fi

note "edge: go-mode system fails when go is unavailable"
fake_go_path="${tmp}/fake-go-path"
mkdir -p "${fake_go_path}"
cat > "${fake_go_path}/go" <<'EOF'
#!/usr/bin/env bash
exit 127
EOF
chmod +x "${fake_go_path}/go"
set +e
PATH="${fake_go_path}:/usr/bin:/bin" "${INSTALLER}" --dry-run --source-dir "${ROOT}" --go-mode system --force --quiet >/dev/null 2>&1
rc=$?
set -e
if [[ $rc -eq 0 ]]; then
  fail "expected --go-mode system to fail when go is unavailable"
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
