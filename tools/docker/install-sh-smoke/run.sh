#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-/workspace/si}"
cd "${SOURCE_DIR}"

installer="${SOURCE_DIR}/tools/install-si.sh"
if [[ ! -f "${installer}" ]]; then
  echo "ERROR: installer not found at ${installer}" >&2
  exit 1
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
install_dir="${work}/bin"
mkdir -p "${install_dir}"

echo "==> root smoke: install from source checkout"
"${installer}" --source-dir "${SOURCE_DIR}" --install-dir "${install_dir}" --force --no-buildx --quiet

target="${install_dir}/si"
if [[ ! -x "${target}" ]]; then
  echo "ERROR: expected binary at ${target}" >&2
  exit 1
fi
"${target}" version
"${target}" --help

echo "==> root smoke: uninstall"
"${installer}" --install-dir "${install_dir}" --uninstall --quiet
if [[ -e "${target}" ]]; then
  echo "ERROR: expected uninstall to remove ${target}" >&2
  exit 1
fi

echo "OK"
