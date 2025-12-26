#!/usr/bin/env bash
set -euo pipefail

SILEXA_ROOT="/opt/silexa"
GIT_NAME="SHi-ON"
GIT_EMAIL="shawn@azdam.com"
NODE_MAJOR=22
K8S_MINOR=${K8S_MINOR:-1.30}
APT_UPDATED=0

require_root() {
  if [[ $(id -u) -ne 0 ]]; then
    echo "This bootstrap must be run as root (try sudo)." >&2
    exit 1
  fi
}

apt_update_once() {
  if [[ $APT_UPDATED -eq 0 ]]; then
    apt-get update -y
    APT_UPDATED=1
  fi
}

install_prereqs() {
  apt_update_once
  apt-get install -y ca-certificates curl gnupg lsb-release git software-properties-common
}

setup_k8s_repo() {
  install -m 0755 -d /etc/apt/keyrings
  if [[ ! -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg ]]; then
    curl -fsSL "https://pkgs.k8s.io/core:/stable:/v${K8S_MINOR}/deb/Release.key" | gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
  fi
  echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v${K8S_MINOR}/deb/ /" > /etc/apt/sources.list.d/kubernetes.list
}

setup_helm_repo() {
  install -m 0755 -d /etc/apt/keyrings
  if [[ ! -f /etc/apt/keyrings/helm.gpg ]]; then
    curl -fsSL https://baltocdn.com/helm/signing.asc | gpg --dearmor -o /etc/apt/keyrings/helm.gpg
  fi
  echo "deb [signed-by=/etc/apt/keyrings/helm.gpg] https://baltocdn.com/helm/stable/debian/ all main" > /etc/apt/sources.list.d/helm-stable-debian.list
}

install_k8s_tools() {
  setup_k8s_repo
  APT_UPDATED=0
  apt_update_once
  apt-get install -y kubectl
  if [[ "${INSTALL_KUBEADM:-0}" == "1" ]]; then
    apt-get install -y kubelet kubeadm
  fi
}

install_helm() {
  setup_helm_repo
  APT_UPDATED=0
  apt_update_once
  apt-get install -y helm
}

setup_node() {
  install -m 0755 -d /etc/apt/keyrings
  if [[ ! -f /etc/apt/keyrings/nodesource.gpg ]]; then
    curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg
    chmod a+r /etc/apt/keyrings/nodesource.gpg
  fi
  echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" > /etc/apt/sources.list.d/nodesource.list
  APT_UPDATED=0
  apt_update_once
  apt-get install -y nodejs
}

setup_git_config() {
  sudo -u "${SUDO_USER:-$(logname)}" git config --global user.name "$GIT_NAME"
  sudo -u "${SUDO_USER:-$(logname)}" git config --global user.email "$GIT_EMAIL"
}

init_directories() {
  mkdir -p "$SILEXA_ROOT" "$SILEXA_ROOT/apps" "$SILEXA_ROOT/agents" "$SILEXA_ROOT/bin"
}

init_repo() {
  if [[ ! -d "$SILEXA_ROOT/.git" ]]; then
    sudo -u "${SUDO_USER:-$(logname)}" git init "$SILEXA_ROOT"
  fi
}

main() {
  require_root
  install_prereqs
  install_k8s_tools
  install_helm
  setup_node
  setup_git_config
  init_directories
  init_repo
  echo "Silexa bootstrap complete. Kubernetes tooling installed."
}

main "$@"
