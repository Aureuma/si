#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-/workspace/si}"
INSTALLER="${SOURCE_DIR}/tools/install-si.sh"

if [[ ! -f "${INSTALLER}" ]]; then
  echo "ERROR: installer not found at ${INSTALLER}" >&2
  exit 1
fi

work="$(mktemp -d)"
cleanup() {
  rm -rf "${work}" || true
}
trap cleanup EXIT

install_dir="${work}/bin"
mkdir -p "${install_dir}"

echo "==> root smoke: install from source checkout"
"${INSTALLER}" \
  --source-dir "${SOURCE_DIR}" \
  --install-dir "${install_dir}" \
  --force \
  --no-buildx \
  --quiet

if [[ ! -x "${install_dir}/si" ]]; then
  echo "ERROR: expected binary at ${install_dir}/si" >&2
  exit 1
fi

"${install_dir}/si" version >/dev/null
"${install_dir}/si" --help >/dev/null

echo "==> root smoke: uninstall"
"${INSTALLER}" --install-dir "${install_dir}" --uninstall --quiet

if [[ -e "${install_dir}/si" ]]; then
  echo "ERROR: expected uninstall to remove ${install_dir}/si" >&2
  exit 1
fi

echo "OK"
