# PaaS Incident Response Runbook

Last updated: 2026-02-17  
Scope: operational response for `si paas` incidents

## Severity Model

| Severity | Definition | Example |
| --- | --- | --- |
| Sev-1 | Active production outage or data-loss risk | all targets failed deploy/rollback, repeated health failures with no known-good rollback |
| Sev-2 | Partial degradation with service impact | one target unhealthy, alert routing broken, webhook ingest failing |
| Sev-3 | Non-critical defect or tooling regression | stale events, warning-only drift reports, documentation mismatch |

## Response Workflow

1. Detect and classify:
   - Check `si paas events list --json` for recent `failed` or `critical` records.
   - Review `si paas alert history --json` for channel delivery and callback hints.
2. Stabilize:
   - Halt risky rollouts (`--continue-on-error=false`, avoid parallel fanout during incident).
   - If needed, execute controlled rollback: `si paas rollback --app <app> --apply`.
3. Diagnose:
   - Inspect per-target logs: `si paas logs --app <app> --target <id> --tail 400`.
   - Reconcile runtime drift: `si paas deploy reconcile --app <app> --json`.
   - Validate target/runtime baseline: `si paas target check --all --json`.
4. Recover:
   - Re-deploy fixed release or rollback to known-good release.
   - Acknowledge incident action trail: `si paas alert acknowledge --id <alert_id>`.
5. Post-incident:
   - Capture root cause, timeline, and follow-up items.
   - Add or update failure drill coverage if new failure mode was discovered.

## Scenario Playbooks

### Deploy failure (`PAAS_REMOTE_*`, `PAAS_HEALTH_CHECK_FAILED`)

1. Run `si paas deploy reconcile --app <app> --json` to identify target states.
2. If health-gated deploy failed, execute rollback:
   - `si paas rollback --app <app> --target <id|all> --apply --json`
3. Validate recovered runtime:
   - `si paas target check --all --json`
   - `si paas logs --app <app> --target <id> --tail 200`

### Blue/green cutover failure

1. Inspect active/previous slots from deploy output fields.
2. Confirm rollback status in deploy failure envelope (`rolled_back_targets`, `target_statuses`).
3. Re-run deploy only after health root cause is fixed:
   - `si paas deploy bluegreen --app <app> --target <id> --apply --json`

### Webhook trigger failure

1. Validate webhook signature and mapping:
   - `si paas deploy webhook map list --json`
2. Reproduce ingest with recorded payload/signature if available.
3. If auth mismatch, rotate secret and update sender config.

### Vault trust/secret guardrail failure

1. Validate trust recipients:
   - `si vault check --file <vault_file>`
2. Re-establish trust:
   - `si vault trust accept ...`
3. Retry deploy without unsafe override unless incident policy explicitly allows.

## Incident Artifact Checklist

1. Timestamped command transcript.
2. Relevant `events list` and `alert history` payload snapshots.
3. Target log excerpts used for diagnosis.
4. Final remediation action and verification command outputs.
5. Follow-up ticket IDs for preventive hardening.
