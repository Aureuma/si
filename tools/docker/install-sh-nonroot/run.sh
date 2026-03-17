#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-/workspace/si}"
cd "${SOURCE_DIR}"

installer="${SOURCE_DIR}/tools/install-si.sh"
if [[ ! -f "${installer}" ]]; then
  echo "ERROR: installer not found at ${installer}" >&2
  exit 1
fi

target="${HOME}/.local/bin/si"

echo "==> non-root smoke: install into user default path"
"${installer}" --source-dir "${SOURCE_DIR}" --force --no-buildx --quiet

if [[ ! -x "${target}" ]]; then
  echo "ERROR: expected binary at ${target}" >&2
  exit 1
fi
"${target}" version
"${target}" --help

echo "==> non-root smoke: uninstall"
"${installer}" --uninstall --quiet
if [[ -e "${target}" ]]; then
  echo "ERROR: expected uninstall to remove ${target}" >&2
  exit 1
fi

echo "OK"
