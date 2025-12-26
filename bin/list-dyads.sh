#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

kube get deployments -l app=silexa-dyad \
  -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.silexa\.dyad}{"\t"}{.metadata.labels.silexa\.department}{"\t"}{.metadata.labels.silexa\.role}{"\t"}{.spec.replicas}{"\n"}{end}' \
  | awk 'BEGIN { printf "%-28s %-12s %-14s %-12s %s\n", "DEPLOYMENT","DYAD","DEPT","ROLE","REPLICAS" } { printf "%-28s %-12s %-14s %-12s %s\n", $1,$2,$3,$4,$5 }'
