## Security hardening checklist

Host-level (Ubuntu LTS):
- **SSH**: disable root login (`PermitRootLogin no`), disable password auth (`PasswordAuthentication no`), non-standard port optional; use key-based auth; enforce `AllowUsers` list.
- **Firewall**: enable UFW (deny by default, allow 22/80/443/8081/8088/9090-9092 as needed) or equivalent iptables rules.
- **Updates**: enable `unattended-upgrades`; run `apt-get update && apt-get upgrade` regularly.
- **Fail2ban**: enable SSH jail; tune ban times; monitor logs.
- **Audit/Logs**: install `auditd`; ship logs to remote (optional); monitor auth.log/syslog.
- **Users**: least-privilege; review `sudo` group; remove unused accounts; use SSH keys only.
- **Filesystem**: no secrets in world-readable paths; keep tokens under `secrets/` with 0600 perms.

Docker/agents:
- Do not mount the Docker socket into actor/critic containers.
- Use dedicated Docker network (`silexa`) and bind service ports to 127.0.0.1 unless explicitly needed.
- Keep container privileges minimal; only the MCP dind sidecar is privileged.
- Secrets access: only `silexa-credentials` should read/write sensitive files; other dyads must request via the credentials broker.

MCP Gateway:
- Runs on streaming transport at 8088; disable/trim catalog servers you do not need to reduce surface area.
- Provide PATs via `secrets/gh_token` and `secrets/stripe_api_key` when enabling those servers.

Recommended next actions:
1) SSH hardening: set `PermitRootLogin no`, `PasswordAuthentication no`, key-only; restart sshd.
2) Enable UFW: `ufw default deny incoming`, allow required ports.
3) Install/configure `unattended-upgrades`, `fail2ban`, `auditd`.
4) Provide Docker Hub PAT as a secret if using dockerhub MCP; otherwise disable that catalog entry.
