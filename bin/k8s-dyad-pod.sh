#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: k8s-dyad-pod.sh <dyad>" >&2
  exit 1
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

DYAD="$1"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

pod=$(kube get pods -l "silexa.dyad=${DYAD}" -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$pod" ]]; then
  echo "no pod found for dyad=${DYAD}" >&2
  exit 1
fi

echo "$pod"
