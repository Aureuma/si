#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: app-status.sh <app-name>" >&2
  exit 1
fi

APP="$1"

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
# shellcheck source=bin/k8s-lib.sh
source "${ROOT_DIR}/bin/k8s-lib.sh"

LABEL="silexa.app=${APP}"

TMP_FILE=$(mktemp)
cleanup() { rm -f "$TMP_FILE"; }
trap cleanup EXIT

if ! kube get deploy,statefulset -l "$LABEL" -o json >"$TMP_FILE" 2>/dev/null; then
  echo "no k8s resources found for app=${APP} (label ${LABEL})" >&2
  exit 1
fi

python3 - <<'PY' "$TMP_FILE" "$APP"
import json
import sys

path = sys.argv[1]
app = sys.argv[2]

data = json.load(open(path, "r", encoding="utf-8"))
items = data.get("items", [])
if not items:
    print(f"no deployments/statefulsets found for app {app}")
    sys.exit(1)

bad = []
for item in items:
    kind = item.get("kind", "")
    name = item.get("metadata", {}).get("name", "")
    spec = item.get("spec", {})
    status = item.get("status", {})
    desired = spec.get("replicas", 1)
    ready = status.get("readyReplicas", 0)
    if desired is None:
        desired = 1
    if ready is None:
        ready = 0
    if ready != desired:
        bad.append((kind, name, ready, desired))

if bad:
    print(f"app {app} replicas not ready:")
    for kind, name, ready, desired in bad:
        print(f"- {kind} {name}: {ready}/{desired}")
    sys.exit(1)

print(f"app {app} replicas healthy")
PY
