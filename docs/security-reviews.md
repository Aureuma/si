## Security reviews (periodic)

Cadence: weekly (lightweight) and monthly (deeper).

Weekly checklist:
- Run `bin/security-audit.sh`; log results to manager `/feedback` (source: security-review).
- Review open access requests and secrets rotations; resolve or escalate.
- Verify firewall/UFW status and exposed ports vs. expected.
- MCP gateway: review enabled servers; disable unused; rotate bearer token if configured.

Monthly checklist:
- Patch/updates: `apt-get update && apt-get upgrade`, verify unattended-upgrades active.
- Review SSH config (root login off, password auth off), fail2ban status, auditd status.
- Docker: confirm sock access limited to intended containers; review compose resource caps.
- Secrets: rotate critical tokens (Telegram, Docker Hub PAT, API keys); validate docker secrets mounts.
- Backups (if any) and log retention.

Runbook:
- Use `bin/management-broadcast.sh "Security review complete: <summary>" warn|info` to notify leads.
- File detailed notes in manager `/feedback` with severity for any findings.
