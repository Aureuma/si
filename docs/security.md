## Security hardening checklist

Host-level (Ubuntu LTS):
- **SSH**: disable root login (`PermitRootLogin no`), disable password auth (`PasswordAuthentication no`), non-standard port optional; use key-based auth; enforce `AllowUsers` list.
- **Firewall**: enable UFW (deny by default, allow 22/80/443/8088 as needed) or equivalent iptables rules.
- **Updates**: enable `unattended-upgrades`; run `apt-get update && apt-get upgrade` regularly.
- **Fail2ban**: enable SSH jail; tune ban times; monitor logs.
- **Audit/Logs**: install `auditd`; ship logs to remote (optional); monitor auth.log/syslog.
- **Users**: least-privilege; review `sudo` group; remove unused accounts; use SSH keys only.
- **Filesystem**: no secrets in world-readable paths; use Kubernetes secrets for tokens.

Kubernetes/agents:
- Use namespace-scoped service accounts and least-privilege RBAC; no pods mount the Docker socket.
- Resource caps: set CPU/memory requests and limits in `infra/k8s/` manifests.
- Secrets: mount from Kubernetes secrets; avoid long-lived env vars for tokens.
- Secrets access: only `silexa-credentials` service account can read/write secrets; other dyads must request via the credentials broker.
- Gatekeeper: enforce policy so pods cannot reference secrets unless running as an allow-listed service account (`silexa-credentials`, `silexa-telegram-bot`, `silexa-mcp-gateway`). See `infra/k8s/gatekeeper/`.
- Network: expose only required ports (manager 9090, brokers 9091/9092, Telegram 8081, MCP 8088). Consider firewall allowlist.

MCP Gateway:
- Runs on streaming transport at 8088; disable/trim catalog servers you don't need to reduce surface.
- Provide PATs as Kubernetes secrets when enabling dockerhub; otherwise remove that server from the catalog.

Automation:
- Run `bin/security-audit.sh` to check SSH/firewall/updates/fail2ban/auditd (non-invasive).
- Optionally add cron to run audit weekly and post results via management-broadcast/Telegram.

Recommended next actions:
1) SSH hardening: set `PermitRootLogin no`, `PasswordAuthentication no`, key-only; restart sshd.
2) Enable UFW: `ufw default deny incoming`, allow required ports (22/80/443/8081/8088/9090-9092).
3) Install/configure `unattended-upgrades`, `fail2ban`, `auditd`.
4) Provide Docker Hub PAT as a secret if using dockerhub MCP; otherwise disable that catalog entry.
5) Run `bin/security-audit.sh` and record findings in manager `/feedback`.
6) Optional tooling: `bin/install-crowdsec.sh`, `bin/install-fail2ban.sh`, `bin/configure-ufw.sh` (run as root).
