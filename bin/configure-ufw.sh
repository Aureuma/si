#!/usr/bin/env bash
set -euo pipefail

# Configure UFW with sane defaults for Silexa.
# Allows: SSH (22), HTTP (80/443), Telegram bot (8081), MCP (8088), manager/brokers (9090-9092).
# Requires sudo/root.

if [[ $EUID -ne 0 ]]; then
  echo "Run as root/sudo." >&2
  exit 1
fi

ufw --force reset
ufw default deny incoming
ufw default allow outgoing

ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 8081/tcp
ufw allow 8088/tcp
ufw allow 9090:9092/tcp

ufw --force enable
ufw status numbered
