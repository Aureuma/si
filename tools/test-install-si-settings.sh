#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HELPER="${ROOT}/tools/install-si-settings.sh"

if [[ ! -x "${HELPER}" ]]; then
  echo "FAIL: helper not found at ${HELPER}" >&2
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

note "creates settings file when missing"
settings1="${tmp}/one/settings.toml"
"${HELPER}" --settings "${settings1}" --default-browser safari
[[ -f "${settings1}" ]] || fail "expected settings file to be created"
grep -q '^\[codex.login\]' "${settings1}" || fail "expected [codex.login] section"
grep -q '^default_browser = "safari"$' "${settings1}" || fail "expected safari default browser"

note "updates existing default_browser"
settings2="${tmp}/two/settings.toml"
mkdir -p "$(dirname "${settings2}")"
cat > "${settings2}" <<'EOT'
[codex.login]
open_url = true
default_browser = "chrome"
EOT
"${HELPER}" --settings "${settings2}" --default-browser safari
grep -q '^open_url = true$' "${settings2}" || fail "expected existing codex.login keys preserved"
grep -q '^default_browser = "safari"$' "${settings2}" || fail "expected default_browser updated"
if [[ "$(grep -c '^default_browser = ' "${settings2}")" -ne 1 ]]; then
  fail "expected exactly one default_browser line"
fi

note "appends codex.login section when absent"
settings3="${tmp}/three/settings.toml"
mkdir -p "$(dirname "${settings3}")"
cat > "${settings3}" <<'EOT'
[codex]
image = "aureuma/si:local"
EOT
"${HELPER}" --settings "${settings3}" --default-browser chrome
grep -q '^\[codex\]$' "${settings3}" || fail "expected existing sections preserved"
grep -q '^\[codex.login\]$' "${settings3}" || fail "expected codex.login appended"
grep -q '^default_browser = "chrome"$' "${settings3}" || fail "expected chrome default browser"

note "rejects unsupported browser"
set +e
"${HELPER}" --settings "${tmp}/bad/settings.toml" --default-browser firefox >/dev/null 2>&1
rc=$?
set -e
if [[ ${rc} -eq 0 ]]; then
  fail "expected unsupported browser to fail"
fi

note "--print renders expected output without mutating file"
settings4="${tmp}/four/settings.toml"
mkdir -p "$(dirname "${settings4}")"
cat > "${settings4}" <<'EOT'
[codex.login]
default_browser = "chrome"
EOT
printed="$("${HELPER}" --settings "${settings4}" --default-browser safari --print)"
grep -q '^default_browser = "chrome"$' "${settings4}" || fail "expected original file unchanged by --print"
printf '%s\n' "${printed}" | grep -q '^default_browser = "safari"$' || fail "expected printed output to reflect requested browser"

note "--check returns success when file is already in desired state"
"${HELPER}" --settings "${settings4}" --default-browser chrome --check

note "--check returns failure when file would change"
set +e
"${HELPER}" --settings "${settings4}" --default-browser safari --check >/dev/null 2>&1
rc=$?
set -e
if [[ ${rc} -eq 0 ]]; then
  fail "expected --check to fail when file is not in desired state"
fi

note "ok"
