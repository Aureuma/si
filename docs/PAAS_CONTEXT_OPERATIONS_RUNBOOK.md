# PaaS Context Operations Runbook

Date: 2026-02-17
Scope: Operational guidance for `internal-dogfood` and `oss-demo` contexts
Owner: Codex

## 1. Purpose

Define the day-2 operating procedure for running `si paas` safely across:

1. `internal-dogfood` (private/internal environments)
2. `oss-demo` (public-safe demo environments)

This runbook enforces strict context boundaries so private operational state and secrets cannot leak into OSS workflows.

## 2. Context Roles

`internal-dogfood`:

1. Real internal validation and pre-production verification
2. Private targets and private operational logs
3. Private vault namespace and restricted operator access

`oss-demo`:

1. Public demo and reproducible sample workflows
2. Disposable/non-sensitive targets only
3. No production credentials, no customer data, no private telemetry

## 3. Required Baseline Setup

1. Set a private state root outside any git workspace:

```bash
export SI_PAAS_STATE_ROOT="$HOME/.si/paas"
```

2. Initialize both contexts:

```bash
si paas context init --name internal-dogfood --type internal-dogfood
si paas context init --name oss-demo --type oss-demo
```

3. Run isolation checks:

```bash
si paas doctor --json
```

## 4. Daily Operating Flow

For `internal-dogfood`:

```bash
si paas --context internal-dogfood target list --json
si paas --context internal-dogfood app list --json
si paas --context internal-dogfood events list --limit 50 --json
```

For `oss-demo`:

```bash
si paas --context oss-demo target list --json
si paas --context oss-demo app list --json
si paas --context oss-demo events list --limit 50 --json
```

Required operator rule:

1. Always pass `--context` explicitly for mutating commands (`deploy`, `rollback`, `secret`, `target`, `context import`).

## 5. Separation Guardrails

1. Never share vault files across `internal-dogfood` and `oss-demo`.
2. Never copy context directories manually between contexts.
3. Use `si paas context export|import` for metadata transfer only.
4. Block deploy if `si paas doctor` reports contamination or secret exposure.
5. Keep backup artifacts encrypted and outside repository roots.

## 6. Deployment and Incident Rules

`internal-dogfood`:

1. Run deploy with full audit/event capture.
2. Treat critical deploy failures as incidents and follow `docs/PAAS_INCIDENT_RUNBOOK.md`.
3. Require rollback readiness before cutover operations.

`oss-demo`:

1. Prefer disposable targets and demo-safe datasets.
2. If failure occurs, prioritize environment reset over forensic restore unless validating incident procedures.

## 7. Backup and Restore Coupling

1. Follow `docs/PAAS_BACKUP_RESTORE_POLICY.md` for backup scope and restore validation.
2. Post-restore, run:

```bash
si paas --context internal-dogfood doctor --json
si paas --context internal-dogfood context show --json
```

3. Do not restore secrets from plaintext artifacts; use vault-native recovery only.

## 8. Weekly Operational Checklist

1. `si paas doctor --json` returns `ok=true` for active state root.
2. `internal-dogfood` and `oss-demo` have distinct targets and vault paths.
3. Latest backup snapshot for each active context is present and checksum-verified.
4. Alert policy and notification channel are configured per context.
5. One rollback drill completed in `internal-dogfood` within the last week.

## 9. Escalation

Escalate immediately when any of these occur:

1. `si paas doctor` detects repo-local private state or secret exposure.
2. Vault mapping for `internal-dogfood` resolves inside repository paths.
3. Cross-context data appears in `target list`, deploy history, or events.
4. Backup checksum verification fails for latest required snapshot.
