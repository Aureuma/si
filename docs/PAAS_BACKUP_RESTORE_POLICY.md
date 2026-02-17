# PaaS Backup and Restore Policy

Date: 2026-02-17
Scope: `si paas` private state roots and audit/event logs
Owner: Codex

## 1. Objective

Define the mandatory backup and restore policy for private `si paas` state so dogfood and customer operations can recover safely without leaking secrets into OSS repositories.

## 2. Protected Data (Per Context)

Back up the following from `<state_root>/contexts/<context>/`:

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

1. `releases/` metadata for recovery acceleration
2. `cache/` only when needed for local forensic replay

## 3. Explicit Exclusions

Never include these in PaaS state backups:

1. `vault/secrets.env` (secret material)
2. Any decrypted secret dumps or plaintext secret export files
3. Runtime data volumes from target nodes (DB volumes, service volumes)

Secrets must be backed up using vault-native policy and encrypted key management controls, not this PaaS state backup policy.

## 4. Backup Frequency and Retention

Minimum baseline:

1. Hourly incremental snapshots for active contexts (`internal-dogfood`, customer contexts)
2. Daily full snapshot
3. Retention: 7 daily + 4 weekly + 3 monthly snapshots
4. Immutable/offsite copy for daily full snapshots

## 5. Backup Procedure (Reference)

Inputs:

1. `SI_PAAS_STATE_ROOT`
2. Context name (`internal-dogfood`, `oss-demo`, `customer-<id>`)

Reference flow:

```bash
STATE_ROOT="${SI_PAAS_STATE_ROOT:-$HOME/.si/paas}"
CTX="internal-dogfood"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
SRC="$STATE_ROOT/contexts/$CTX"
OUT="$HOME/.si/paas-backups/$CTX/$TS"

mkdir -p "$OUT"
rsync -a --delete \
  --exclude 'vault/secrets.env' \
  --exclude '*.secret' \
  "$SRC/" "$OUT/"

tar -C "$OUT" -czf "$OUT.tar.gz" .
sha256sum "$OUT.tar.gz" > "$OUT.tar.gz.sha256"
```

## 6. Restore Procedure (Reference)

Before restore:

1. Confirm target context and incident ticket ID.
2. Confirm snapshot checksum.
3. Confirm vault trust/recipient state separately.

Restore flow:

```bash
STATE_ROOT="${SI_PAAS_STATE_ROOT:-$HOME/.si/paas}"
CTX="internal-dogfood"
SNAPSHOT="/secure-backups/$CTX/<timestamp>.tar.gz"
RESTORE_DIR="$STATE_ROOT/contexts/$CTX"

mkdir -p "$RESTORE_DIR"
tar -C "$RESTORE_DIR" -xzf "$SNAPSHOT"
```

Post-restore validation (required):

1. `si paas doctor --json` must return `"ok": true`.
2. `si paas context show --name <context> --json` must resolve expected context metadata.
3. `si paas events list --limit 20 --json` must read restored event logs.
4. Run targeted deploy/reconcile dry-run before production writes.

## 7. Governance Requirements

1. Backups must remain outside git workspaces.
2. Backups must use encrypted storage and controlled access.
3. Every restore must produce an incident artifact with:
   - snapshot ID and checksum
   - operator identity
   - restore start/end timestamps
   - post-restore doctor result
4. Quarterly restore drills are mandatory for `internal-dogfood`.

## 8. Failure Modes and Actions

1. Missing snapshot checksum:
   - Do not restore.
   - Escalate and recover from previous verified snapshot.
2. `si paas doctor` fails post-restore:
   - Block deploy and secret-mutating commands.
   - Resolve contamination/secret findings first.
3. Vault secret mismatch after restore:
   - Reconcile with vault backup policy; do not copy plaintext secrets into PaaS state roots.
