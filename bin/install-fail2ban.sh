#!/usr/bin/env bash
set -euo pipefail

# Install and configure Fail2ban for SSH on Ubuntu/Debian.
# Requires sudo/root.

if [[ $EUID -ne 0 ]]; then
  echo "Run as root/sudo." >&2
  exit 1
fi

apt-get update -y
apt-get install -y fail2ban

JAIL_LOCAL="/etc/fail2ban/jail.local"
if [[ ! -f "$JAIL_LOCAL" ]]; then
  cat >"$JAIL_LOCAL" <<'EOF'
[DEFAULT]
bantime = 1h
findtime = 10m
maxretry = 5
backend = systemd

[sshd]
enabled = true
port    = ssh
logpath = %(sshd_log)s
EOF
fi

systemctl enable --now fail2ban
systemctl restart fail2ban

echo "Fail2ban status:"
fail2ban-client status sshd || true
