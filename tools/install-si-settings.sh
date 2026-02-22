#!/usr/bin/env bash
set -euo pipefail

# Helper for mutating/checking ~/.si/settings.toml installer login browser defaults.
# Intended for both installer runtime use and CI regression checks.

usage() {
  cat <<'USAGE'
Usage:
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome>
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --print
  tools/install-si-settings.sh --settings <path> --default-browser <safari|chrome> --check
USAGE
}

trim() {
  echo "$1" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

SETTINGS_PATH=""
DEFAULT_BROWSER=""
MODE="write"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --settings)
      SETTINGS_PATH="$2"
      shift 2
      ;;
    --default-browser)
      DEFAULT_BROWSER="$2"
      shift 2
      ;;
    --print)
      MODE="print"
      shift
      ;;
    --check)
      MODE="check"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

SETTINGS_PATH="$(trim "${SETTINGS_PATH}")"
DEFAULT_BROWSER="$(echo "$(trim "${DEFAULT_BROWSER}")" | tr '[:upper:]' '[:lower:]')"

if [[ -z "${SETTINGS_PATH}" ]]; then
  echo "--settings is required" >&2
  exit 1
fi

case "${DEFAULT_BROWSER}" in
  safari|chrome) ;;
  *)
    echo "--default-browser must be safari or chrome" >&2
    exit 1
    ;;
esac

mkdir -p "$(dirname "${SETTINGS_PATH}")"
rendered="$(mktemp)"
cleanup() {
  rm -f "${rendered}" || true
}
trap cleanup EXIT

if [[ ! -f "${SETTINGS_PATH}" ]]; then
  cat > "${rendered}" <<EOT
[codex.login]
default_browser = "${DEFAULT_BROWSER}"
EOT
else
  awk -v browser="${DEFAULT_BROWSER}" '
BEGIN {
  in_login=0
  saw_login=0
  wrote=0
}
{
  line=$0
  is_header = match(line, /^[[:space:]]*\[[^]]+\][[:space:]]*$/)
  if (is_header) {
    if (in_login && !wrote) {
      print "default_browser = \"" browser "\""
      wrote=1
    }
    if (line ~ /^[[:space:]]*\[codex\.login\][[:space:]]*$/) {
      in_login=1
      saw_login=1
    } else {
      in_login=0
    }
    print line
    next
  }
  if (in_login && line ~ /^[[:space:]]*default_browser[[:space:]]*=/) {
    if (!wrote) {
      print "default_browser = \"" browser "\""
      wrote=1
    }
    next
  }
  print line
}
END {
  if (saw_login && !wrote) {
    print "default_browser = \"" browser "\""
  }
  if (!saw_login) {
    print ""
    print "[codex.login]"
    print "default_browser = \"" browser "\""
  }
}
' "${SETTINGS_PATH}" > "${rendered}"
fi

case "${MODE}" in
  print)
    cat "${rendered}"
    ;;
  check)
    if [[ ! -f "${SETTINGS_PATH}" ]]; then
      exit 1
    fi
    if cmp -s "${SETTINGS_PATH}" "${rendered}"; then
      exit 0
    fi
    exit 1
    ;;
  write)
    mv "${rendered}" "${SETTINGS_PATH}"
    trap - EXIT
    ;;
  *)
    echo "unknown mode: ${MODE}" >&2
    exit 1
    ;;
esac
