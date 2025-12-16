#!/usr/bin/env bash
set -euo pipefail

# Lightweight, non-invasive security audit.
# Reports SSH hardening, firewall, updates, docker socket perms, and key services.

report() { echo "[$(date -u +'%Y-%m-%dT%H:%M:%SZ')] $*"; }

check_ssh() {
  if ! command -v sshd >/dev/null 2>&1; then
    report "SSHD: not installed"
    return
  }
  conf=$(sshd -T 2>/dev/null || true)
  prl=$(printf '%s\n' "$conf" | awk '/^permitrootlogin/ {print $2}')
  pass=$(printf '%s\n' "$conf" | awk '/^passwordauthentication/ {print $2}')
  port=$(printf '%s\n' "$conf" | awk '/^port / {print $2}')
  report "SSHD: port=${port:-unknown} PermitRootLogin=${prl:-unknown} PasswordAuthentication=${pass:-unknown}"
}

check_firewall() {
  if command -v ufw >/dev/null 2>&1; then
    status=$(ufw status 2>/dev/null | head -n1 || true)
    report "UFW: ${status:-unknown}"
  elif command -v iptables >/dev/null 2>&1; then
    chains=$(iptables -L 2>/dev/null | head -n 3 | tr '\n' ' ' || true)
    report "iptables present: ${chains}"
  else
    report "Firewall: none detected (ufw/iptables missing)"
  fi
}

check_updates() {
  if command -v apt-get >/dev/null 2>&1; then
    pending=$(apt-get -s upgrade 2>/dev/null | awk '/^0 upgraded, 0 newly/{print "clean"}')
    if [[ -z "$pending" ]]; then
      report "Updates: pending (see apt-get upgrade -s)"
    else
      report "Updates: clean"
    fi
  fi
}

check_unattended() {
  if dpkg -s unattended-upgrades >/dev/null 2>&1; then
    report "unattended-upgrades: installed"
  else
    report "unattended-upgrades: missing"
  fi
}

check_fail2ban() {
  if command -v fail2ban-client >/dev/null 2>&1; then
    status=$(fail2ban-client status 2>/dev/null | head -n1 || true)
    report "fail2ban: ${status:-installed}"
  else
    report "fail2ban: missing"
  fi
}

check_docker_sock() {
  if [[ -S /var/run/docker.sock ]]; then
    perms=$(stat -c '%A %G' /var/run/docker.sock)
    report "docker.sock: ${perms}"
  else
    report "docker.sock: not present"
  fi
}

check_sudoers() {
  admins=$(getent group sudo 2>/dev/null | cut -d: -f4)
  report "sudo group members: ${admins:-none}"
}

check_auditd() {
  if dpkg -s auditd >/dev/null 2>&1; then
    report "auditd: installed"
  else
    report "auditd: missing"
  fi
}

check_ssh
check_firewall
check_updates
check_unattended
check_fail2ban
check_docker_sock
check_sudoers
check_auditd
