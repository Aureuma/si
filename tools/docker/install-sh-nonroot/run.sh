#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-/workspace/si}"
INSTALLER="${SOURCE_DIR}/tools/install-si.sh"
TARGET="${HOME}/.local/bin/si"

if [[ ! -f "${INSTALLER}" ]]; then
  echo "ERROR: installer not found at ${INSTALLER}" >&2
  exit 1
fi

echo "==> non-root smoke: install into user default path"
"${INSTALLER}" \
  --source-dir "${SOURCE_DIR}" \
  --force \
  --no-buildx \
  --quiet

if [[ ! -x "${TARGET}" ]]; then
  echo "ERROR: expected binary at ${TARGET}" >&2
  exit 1
fi

"${TARGET}" version >/dev/null
"${TARGET}" --help >/dev/null

echo "==> non-root smoke: uninstall"
"${INSTALLER}" --uninstall --quiet

if [[ -e "${TARGET}" ]]; then
  echo "ERROR: expected uninstall to remove ${TARGET}" >&2
  exit 1
fi

echo "OK"
