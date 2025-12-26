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
- Kubernetes: review service accounts/RBAC and resource requests/limits in `infra/k8s/`.
- Secrets: rotate critical tokens (Telegram, Docker Hub PAT, API keys); validate Kubernetes secret mounts.
- Backups (if any) and log retention.

Runbook:
- Use `bin/management-broadcast.sh "Security review complete: <summary>" warn|info` to notify leads.
- File detailed notes in manager `/feedback` with severity for any findings.
