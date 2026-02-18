#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SMOKE_IMAGE="${SI_INSTALL_SMOKE_IMAGE:-si-install-smoke:local}"
NONROOT_IMAGE="${SI_INSTALL_NONROOT_IMAGE:-si-install-nonroot:local}"
SKIP_NONROOT="${SI_INSTALL_SMOKE_SKIP_NONROOT:-0}"
SOURCE_DIR="${SI_INSTALL_SOURCE_DIR:-${ROOT_DIR}}"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  cat <<'USAGE'
Usage: ./tools/test-install-si-docker.sh

Builds and runs installer smoke tests in Docker:
- root install/uninstall
- non-root install/uninstall

Environment overrides:
  SI_INSTALL_SMOKE_IMAGE
  SI_INSTALL_NONROOT_IMAGE
  SI_INSTALL_SOURCE_DIR
  SI_INSTALL_SMOKE_SKIP_NONROOT=1
USAGE
  exit 0
fi

if [[ "$#" -gt 0 ]]; then
  echo "unexpected arguments: $*" >&2
  echo "Run ./tools/test-install-si-docker.sh --help for usage." >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "SKIP: docker is not available; skipping Docker installer smoke tests" >&2
  exit 0
fi

if [[ ! -f "${SOURCE_DIR}/tools/install-si.sh" ]]; then
  echo "FAIL: installer not found under source dir: ${SOURCE_DIR}" >&2
  exit 1
fi

docker_build_image() {
  local image="$1"
  local dockerfile="$2"
  local context="$3"
  if docker buildx version >/dev/null 2>&1; then
    docker buildx build --load \
      -t "${image}" \
      -f "${dockerfile}" \
      "${context}"
    return 0
  fi
  echo "⚠️  docker buildx is not available; falling back to docker build" >&2
  docker build \
    -t "${image}" \
    -f "${dockerfile}" \
    "${context}"
}

echo "==> Build root smoke image: ${SMOKE_IMAGE}"
docker_build_image \
  "${SMOKE_IMAGE}" \
  "${ROOT_DIR}/tools/docker/install-sh-smoke/Dockerfile" \
  "${ROOT_DIR}/tools/docker/install-sh-smoke"

echo "==> Run root installer smoke"
docker run --rm -t \
  -v "${SOURCE_DIR}:/workspace/si:ro" \
  -e SI_INSTALL_SOURCE_DIR=/workspace/si \
  "${SMOKE_IMAGE}"

if [[ "${SKIP_NONROOT}" == "1" ]]; then
  echo "==> Skip non-root smoke (SI_INSTALL_SMOKE_SKIP_NONROOT=1)"
  exit 0
fi

echo "==> Build non-root smoke image: ${NONROOT_IMAGE}"
docker_build_image \
  "${NONROOT_IMAGE}" \
  "${ROOT_DIR}/tools/docker/install-sh-nonroot/Dockerfile" \
  "${ROOT_DIR}/tools/docker/install-sh-nonroot"

echo "==> Run non-root installer smoke"
docker run --rm -t \
  -v "${SOURCE_DIR}:/workspace/si:ro" \
  -e SI_INSTALL_SOURCE_DIR=/workspace/si \
  "${NONROOT_IMAGE}"
