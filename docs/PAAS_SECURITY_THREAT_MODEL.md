# PaaS Security Review Checklist and Threat Model

Last updated: 2026-02-17  
Scope: `si paas` CLI control-plane workflows in `tools/si`

## Trust Boundaries

1. Local operator machine and context-scoped state root (`SI_PAAS_STATE_ROOT`).
2. Vault-managed secret material (`si vault` files and trust state).
3. Remote VPS targets over SSH/SCP transport.
4. External webhook/notifier surfaces (Git webhook payloads, Telegram API).

## Critical Assets

1. Target inventory and deployment metadata (`targets.json`, `deployments.json`, event logs).
2. Secrets and secret-derived references (vault files, environment material).
3. Release bundles and compose artifacts.
4. Alert/audit streams used for incident response decisions.

## Threat Model (STRIDE-oriented)

| Threat | Example in PaaS Context | Existing Controls | Residual Risk |
| --- | --- | --- | --- |
| Spoofing | Forged webhook deploy trigger | HMAC signature validation and explicit mapping checks in webhook ingest flow | Secret rotation hygiene remains operator responsibility |
| Tampering | Malicious compose fragment overriding existing service definitions | Add-on merge validation with `additive_no_override`; deterministic bundle generation | Untrusted local filesystem access can still alter source compose before deploy |
| Repudiation | Operator action without traceability | Unified audit event model (`events/audit.jsonl`) for success/failure command paths | Local log retention policy must be maintained by operator |
| Information Disclosure | Secrets printed to terminal/JSON output | Sensitive-field redaction, plaintext guardrails, vault trust checks, context export secret-key rejection | Unsafe override flags can bypass protections if misused |
| Denial of Service | Broken deploy rollout destabilizing all targets | Strategy-aware fan-out (`canary`/`rolling`), health gates, rollback orchestration, blue/green rollback | Large-scale target outages still require manual incident response |
| Elevation of Privilege | Command injection via remote shell templating | Controlled command construction, quoted remote arguments, fixed command surfaces | Operator-provided custom cutover commands can be unsafe if misconfigured |

## Security Review Checklist (Per PR touching `si paas`)

1. Authentication and trust:
   - Webhook/auth integrations validate signatures or equivalent trust checks.
   - New remote operations do not bypass vault trust/recipient guardrails.
2. Secret handling:
   - No plaintext secrets are added to tracked state or default output.
   - Redaction paths cover newly introduced output fields.
3. Input validation:
   - New flags/inputs are bounded, validated, and return deterministic failure codes.
   - Unknown magic placeholders/unsafe merge cases fail closed.
4. State isolation:
   - Reads/writes remain context-scoped under `contexts/<ctx>/...`.
   - No cross-context implicit data sharing is introduced.
5. Remote execution safety:
   - SSH/SCP commands use quoted arguments and least-required command scope.
   - Rollback and failure paths are tested for non-destructive recovery.
6. Auditability:
   - Success/failure actions are captured in audit/deploy/alert event streams.
   - High-risk operations emit remediation hints on failure.
7. Regression coverage:
   - Relevant matrix commands in `docs/PAAS_TEST_MATRIX.md` are run.
   - Failure drills in `docs/PAAS_FAILURE_DRILLS.md` are run for deploy/runtime changes.

## Review Outcome Template

Use this template in ticket notes/review comments:

```text
Security Review (WS09-03):
- Scope checked:
- New/changed trust boundaries:
- Checklist items verified:
- Residual risks accepted:
- Required follow-up hardening:
```
