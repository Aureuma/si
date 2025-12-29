#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATEKEEPER_VERSION="${GATEKEEPER_VERSION:-v3.15.0}"
MANIFEST_URL="https://raw.githubusercontent.com/open-policy-agent/gatekeeper/${GATEKEEPER_VERSION}/deploy/gatekeeper.yaml"
TEMPLATE_FILE="${ROOT_DIR}/infra/k8s/gatekeeper/templates/restrict-secret-refs.yaml"
CONSTRAINT_FILE="${ROOT_DIR}/infra/k8s/gatekeeper/constraints/silexa-dyad-secret-refs.yaml"

if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required" >&2
  exit 1
fi

kubectl apply -f "$MANIFEST_URL"

kubectl -n gatekeeper-system rollout status deployment/gatekeeper-controller-manager --timeout=180s

kubectl wait --for=condition=Established crd/constrainttemplates.templates.gatekeeper.sh --timeout=180s
kubectl apply -f "$TEMPLATE_FILE"

until kubectl get crd/k8srestrictsecretrefs.constraints.gatekeeper.sh >/dev/null 2>&1; do
  sleep 2
done
kubectl wait --for=condition=Established crd/k8srestrictsecretrefs.constraints.gatekeeper.sh --timeout=180s
kubectl apply -f "$CONSTRAINT_FILE"

echo "gatekeeper installed and policies applied"
