#!/usr/bin/env bash
set -euo pipefail

# Install CrowdSec agent and iptables bouncer on Ubuntu/Debian.
# Requires sudo/root. Idempotent where possible.

if [[ $EUID -ne 0 ]]; then
  echo "Run as root/sudo." >&2
  exit 1
fi

echo "Installing CrowdSec..."
curl -fsSL https://packagecloud.io/install/repositories/crowdsec/crowdsec/script.deb.sh | bash
apt-get update -y
apt-get install -y crowdsec crowdsec-firewall-bouncer-iptables

echo "Enabling services..."
systemctl enable --now crowdsec
systemctl enable --now crowdsec-firewall-bouncer

echo "Status:"
systemctl status --no-pager crowdsec || true
systemctl status --no-pager crowdsec-firewall-bouncer || true

echo "CrowdSec installed. Consider adding HTTP/SSH scenarios and tuning collections."
