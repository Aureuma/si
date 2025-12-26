## Security hardening checklist

Host-level (Ubuntu LTS):
- **SSH**: disable root login (`PermitRootLogin no`), disable password auth (`PasswordAuthentication no`), non-standard port optional; use key-based auth; enforce `AllowUsers` list.
- **Firewall**: enable UFW (deny by default, allow 22/80/443/8088 as needed) or equivalent iptables rules.
- **Updates**: enable `unattended-upgrades`; run `apt-get update && apt-get upgrade` regularly.
- **Fail2ban**: enable SSH jail; tune ban times; monitor logs.
- **Audit/Logs**: install `auditd`; ship logs to remote (optional); monitor auth.log/syslog.
- **Users**: least-privilege; review `sudo` group; remove unused accounts; use SSH keys only.
- **Filesystem**: no secrets in world-readable paths; use docker secrets for tokens.

Docker/agents:
- Limit docker.sock access to actors/critics/coder only (already applied); keep mcp-gateway/dockerhub aware of PATs via secrets.
- Resource caps: Swarm CPU/mem limits set for all services.
- Secrets: mount from `secrets/` via docker secrets; avoid long-lived env vars.
- Network: expose only required ports (manager 9090, brokers 9091/9092, Telegram 8081, MCP 8088). Consider firewall allowlist.

MCP Gateway:
- Runs on streaming transport at 8088; disable/trim catalog servers you don't need to reduce surface.
- Provide PATs as docker secrets when enabling dockerhub; otherwise remove that server from the catalog.

Automation:
- Run `bin/security-audit.sh` to check SSH/firewall/updates/fail2ban/auditd and docker.sock perms (non-invasive).
- Optionally add cron to run audit weekly and post results via management-broadcast/Telegram.

Recommended next actions:
1) SSH hardening: set `PermitRootLogin no`, `PasswordAuthentication no`, key-only; restart sshd.
2) Enable UFW: `ufw default deny incoming`, allow required ports (22/80/443/8081/8088/9090-9092).
3) Install/configure `unattended-upgrades`, `fail2ban`, `auditd`.
4) Provide Docker Hub PAT as a secret if using dockerhub MCP; otherwise disable that catalog entry.
5) Run `bin/security-audit.sh` and record findings in manager `/feedback`.
6) Optional tooling: `bin/install-crowdsec.sh`, `bin/install-fail2ban.sh`, `bin/configure-ufw.sh` (run as root).
