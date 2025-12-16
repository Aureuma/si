## Recommended security tooling (host and agents)

Host / network:
- **CrowdSec**: collaborative IPS/IDS; good fit for SSH/HTTP brute-force. Install agent + bouncers (iptables/ufw). Low resource footprint.
- **Fail2ban**: lightweight SSH/HTTP jailer. Simpler than CrowdSec; already noted in security checklist. Use for quick SSH hardening if CrowdSec not installed.
- **auditd**: OS audit trails; keep enabled for auth/syscalls of interest.
- **UFW**: simple firewall policy (default deny; allow only needed ports).

Visibility / inventory:
- **osquery**: host inventory and configuration queries; useful for periodic compliance checks.

Containers / Go services:
- **zap** / **logrus** with structured logging (already standard) plus `net/http/pprof` gated by auth if profiling is needed.
- **tcell/seccomp** (via Docker runtime options) to sandbox containers; consider adding seccomp/apparmor profiles for higher risk services.

Selection guidance:
- Prefer **CrowdSec** if you want collaborative ban-lists and HTTP protections; otherwise use **Fail2ban** for SSH-only basics.
- Keep **auditd** running; add **osquery** if you need scheduled config checks.
- Apply **UFW** rules regardless of other tools.

Next steps (optional):
- Install CrowdSec with iptables/ufw bouncer; add basic HTTP/SSH collections.
- Or, configure Fail2ban SSH jail (if not using CrowdSec) with ban-time tuned.
- Add osquery for periodic config snapshots; feed results into manager feedback.
