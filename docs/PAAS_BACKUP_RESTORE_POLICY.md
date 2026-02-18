# PaaS Backup and Restore Policy

Date: 2026-02-18
Scope: `si paas` state roots and Supabase self-hosted backup workflows
Owner: Codex

## 1. Objective

Define mandatory backup and restore policy for:

1. Private `si paas` state.
2. Supabase self-hosted PostgreSQL backups operated through WAL-G.
3. Databasus metadata-sidecar support (without public host-web exposure).

## 2. Protected data (per PaaS context)

Back up from `<state_root>/contexts/<context>/`:

1. `config.json`
2. `targets.json`
3. `deployments.json`
4. `addons.json`
5. `bluegreen.json`
6. `webhooks/mappings.json`
7. `alerts/policy.json`
8. `events/deployments.jsonl`
9. `events/alerts.jsonl`
10. `events/audit.jsonl`

Optional:

1. `releases/` metadata (for faster recovery)
2. `cache/` (only for forensics)

## 3. Explicit exclusions

Never include in PaaS state backup bundles:

1. `vault/secrets.env`
2. Any plaintext secret exports or debug dumps
3. Runtime data volumes copied outside backup policy controls

Secrets remain governed by vault-native controls.

## 4. Supabase backup contract

`si` defines the `supabase-self-hosted` backup profile:

- Recommended addon packs: `supabase-walg`, `databasus`
- Default run service: `supabase-walg-backup`
- Default restore service: `supabase-walg-restore`
- Required env: `WALG_S3_PREFIX`, `WALG_AWS_ACCESS_KEY_ID`, `WALG_AWS_SECRET_ACCESS_KEY`, `WALG_AWS_ENDPOINT`

Check contract:

```bash
si paas backup contract
si paas backup contract --json
```

## 5. Backup frequency and retention

Minimum baseline:

1. Hourly WAL-G incremental backup push for active apps.
2. Daily verified restore test in non-production target.
3. Retention: 7 daily + 4 weekly + 3 monthly snapshots.
4. Immutable offsite copy for daily full snapshot lineage.

## 6. Backup procedure (reference)

Run backup:

```bash
si paas backup run --app <slug>
```

Check status:

```bash
si paas backup status --app <slug>
```

Example explicit service and timeout:

```bash
si paas backup run --app <slug> --service supabase-walg-backup --timeout 3m --json
```

## 7. Restore procedure (reference)

Restore latest:

```bash
si paas backup restore --app <slug> --from LATEST --force
```

Restore specific backup id:

```bash
si paas backup restore --app <slug> --from <backup-id> --force --json
```

Post-restore required checks:

1. `si paas doctor --json` returns `ok=true`.
2. `si paas app status --app <slug> --json` reflects expected release.
3. `si paas events list --limit 20 --json` shows restore and verification trail.
4. App-level health checks pass before production writes resume.

## 8. Governance requirements

1. Backups must be encrypted at rest and in transit.
2. Backup artifacts must stay outside git workspaces.
3. Databasus must remain private-only (no host web port exposure).
4. Every restore must log:
   - backup id/checksum
   - operator identity
   - start/end timestamps
   - post-restore validation outputs
5. Quarterly restore drills are mandatory for production contexts.

## 9. Failure modes and actions

1. Missing or unverified backup id/checksum:
   - block restore
   - use last verified snapshot
2. Failed post-restore doctor checks:
   - block deploy/secret mutations
   - resolve contamination and rerun checks
3. Vault mismatch after restore:
   - reconcile via vault workflows only
   - never inject plaintext secrets into PaaS state roots
