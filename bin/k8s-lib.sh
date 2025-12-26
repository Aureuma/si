#!/usr/bin/env bash
set -euo pipefail

k8s_namespace() {
  echo "${SILEXA_NAMESPACE:-silexa}"
}

k8s_kubeconfig() {
  if [[ -n "${KUBECONFIG:-}" ]]; then
    echo "--kubeconfig ${KUBECONFIG}"
  fi
}

kube() {
  local ns
  ns="$(k8s_namespace)"
  # shellcheck disable=SC2046
  kubectl $(k8s_kubeconfig) -n "$ns" "$@"
}
