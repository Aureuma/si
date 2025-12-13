#!/usr/bin/env bash
set -euo pipefail

SILEXA_ROOT="/opt/silexa"
GIT_NAME="SHi-ON"
GIT_EMAIL="shawn@azdam.com"
NODE_MAJOR=22
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

setup_docker_repo() {
  install -m 0755 -d /etc/apt/keyrings
  if [[ ! -f /etc/apt/keyrings/docker.gpg ]]; then
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
  fi
  local codename
  codename=$(lsb_release -cs)
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu ${codename} stable" > /etc/apt/sources.list.d/docker.list
}

install_docker() {
  setup_docker_repo
  APT_UPDATED=0
  apt_update_once
  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  systemctl enable docker containerd
  systemctl restart docker || systemctl start docker
  local target_user
  target_user=${SUDO_USER:-$(logname)}
  if id "${target_user}" &>/dev/null; then
    if ! id -nG "${target_user}" | grep -q "docker"; then
      usermod -aG docker "${target_user}"
      echo "Added ${target_user} to docker group (log out/in to take effect)."
    fi
  fi
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
  install_docker
  setup_node
  setup_git_config
  init_directories
  init_repo
  echo "Silexa bootstrap complete. You may need to re-login for docker group membership."
}

main "$@"
