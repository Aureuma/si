# Ticket: `si cloudflare` Full Cloudflare Integration (Vault-Compatible, Multi-Account)

Date: 2026-02-08
Owner: Unassigned
Primary Goal: Add `si cloudflare ...` as a first-class command family for Cloudflare control, monitoring, and operations across common products using a secure, vault-compatible credential model and consistent `si` UX.

## 0. Decision Lock (Initial)

This plan is explicitly locked to:

1. `si cloudflare` as the canonical command (with optional alias `si cf`).
2. API token authentication only for the CLI runtime (no legacy global API key default path).
3. Official Cloudflare Go SDK as the typed bridge where practical, with raw REST fallback for parity.
4. Vault-compatible credential resolution (`si vault run` compatibility) and no secret persistence in git-tracked settings.
5. Multi-account and multi-environment context model, where environments map to account/zone defaults (not a Cloudflare-internal sandbox mode).

Rationale:

- Minimizes auth risk and aligns with least-privilege token scopes.
- Enables broad product coverage while preserving endpoint parity.
- Matches existing `si` architecture patterns used in `si stripe` and `si github`.

## 1. Requirement Understanding (What Must Be Delivered)

This ticket introduces:

- `si cloudflare ...`

It must support common Cloudflare operations comprehensively, including:

1. Account/zone context and auth diagnostics.
2. Zone and DNS CRUD.
3. SSL/TLS and certificate-related controls.
4. Cache controls and purge workflows.
5. Rules and security controls (WAF/rulesets/firewall/rate limits as available by entitlement).
6. Workers and Pages workflows.
7. Data platform operations (R2, D1, KV, Queues) for common CRUD/ops paths.
8. Zero Trust Access and Tunnel essentials.
9. Load balancer/pool/health-check essentials.
10. Analytics/reporting and logs job workflows.
11. Raw endpoint fallback for unsupported/long-tail APIs.

Cross-cutting requirements:

- Credentials must come from `si vault` or be fully compatible with `tickets/creds-management-integration-plan.md`.
- Output and interaction must follow existing `si` conventions:
  - clear colors in human mode
  - deterministic `--json` mode (no mixed banner noise)
  - safe confirmations for destructive commands
- Errors must include actionable Cloudflare API details with secret redaction.

## 2. Definition Of Done

Implementation is complete when all are true:

1. `si cloudflare` is wired in dispatch/help.
2. Vault-compatible credential and context resolution is implemented.
3. Multi-account context commands are implemented and persisted in settings.
4. Multi-environment mapping (`prod|staging|dev`) is implemented as account/zone context defaults.
5. Core product commands are implemented for common operations:
   - zone/dns/security/cache/ssl/workers/pages/r2/d1/kv/queues/tunnel/access/lb.
6. Raw fallback command supports unsupported endpoints.
7. Reporting/analytics commands provide useful operational output.
8. All command families support strict `--json` output.
9. Redaction and safety/confirmation policies are consistently applied.
10. Unit + integration tests cover parsing, auth/context, bridge behavior, and error paths.
11. E2E-style subprocess tests (mock API) validate end-to-end token flow + representative command actions.
12. Docs (`README`, settings reference, dedicated guide) are updated and usable.

## 3. Cloudflare Auth & API Policy

### 3.1 API surface

- Cloudflare API is the capability surface.
- Command handlers remain capability-oriented.
- Auth and token lifecycle are handled by runtime context/provider layers.

### 3.2 Auth policy

- API token only for `si cloudflare` runtime operations.
- No default support for legacy global API key + email mode.
- Token scope validation via `auth status` / `doctor` commands.

### 3.3 Base URL and platform scope

- Default API base URL: `https://api.cloudflare.com/client/v4`.
- Support override for enterprise/edge variants via settings/env.

## 4. Vault Compatibility Contract (Mandatory)

Follow `tickets/creds-management-integration-plan.md` principles:

1. Secrets encrypted at rest in vault repo (`vault/.env.<env>` pattern).
2. No plaintext secret persistence in repo/settings.
3. Runtime decryption in-memory when possible.
4. Compatibility with `si vault run -- ...` key injection.

### 4.1 Credential source contract

Primary source:

- Native vault resolver (future).

Compatibility source:

- Environment variables with canonical names so keys can be supplied via `si vault run`.

### 4.2 Canonical secret key names

Global keys:

- `CLOUDFLARE_API_BASE_URL` (optional)
- `CLOUDFLARE_DEFAULT_ACCOUNT` (optional)
- `CLOUDFLARE_DEFAULT_ENV` (optional, context env label)

Per-account keys:

- `CLOUDFLARE_<ACCOUNT>_API_TOKEN`
- `CLOUDFLARE_<ACCOUNT>_ACCOUNT_ID`
- `CLOUDFLARE_<ACCOUNT>_DEFAULT_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_DEFAULT_ZONE_NAME`
- `CLOUDFLARE_<ACCOUNT>_PROD_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_STAGING_ZONE_ID`
- `CLOUDFLARE_<ACCOUNT>_DEV_ZONE_ID`

Compatibility fallback keys:

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_ZONE_ID`

### 4.3 Settings model (non-secret pointers only)

`settings.toml` additions:

- `[cloudflare]`
  - `default_account`
  - `default_env` (`prod|staging|dev`)
  - `api_base_url`
  - `log_file`
  - `vault_env`
  - `vault_file`
- `[cloudflare.accounts.<alias>]`
  - `name`
  - `account_id`
  - `account_id_env`
  - `api_base_url`
  - `vault_prefix`
  - `default_zone_id`
  - `default_zone_name`
  - `prod_zone_id`
  - `staging_zone_id`
  - `dev_zone_id`
  - `api_token_env`

Settings must not store raw token values.

## 5. Context Model (Multi-Account + Multi-Environment)

### 5.1 Account model

- Multiple Cloudflare accounts under one operator org are supported.
- One account selected as current default for commands.

### 5.2 Environment model

- `prod`, `staging`, `dev` are context labels.
- They map to zone/account defaults configured in settings/env.
- This is a CLI context abstraction, not a Cloudflare service environment switch.

### 5.3 Zone model

- Commands accept explicit `--zone`/`--zone-id` overrides.
- If omitted, resolve from context env -> account mapping -> default zone.

## 6. Command Surface (Comprehensive Common Coverage)

### 6.1 Auth / Context

- `si cloudflare auth status [--account <alias>] [--json]`
- `si cloudflare context list [--json]`
- `si cloudflare context current [--json]`
- `si cloudflare context use --account <alias> [--env prod|staging|dev] [--zone <zone>] [--base-url <url>]`
- `si cloudflare doctor [--account <alias>] [--json]`

### 6.2 Zone / DNS

- `si cloudflare zone list|get|create|update|delete ...`
- `si cloudflare dns list|get|create|update|delete ...`
- `si cloudflare dns import|export ...`

### 6.3 SSL/TLS / Certificates

- `si cloudflare tls get|set ...`
- `si cloudflare cert list|get|upload|delete ...`
- `si cloudflare origin-cert create|list|revoke ...`

### 6.4 Cache

- `si cloudflare cache purge --everything|--tags ...|--hosts ...|--prefixes ...`
- `si cloudflare cache settings get|set ...`

### 6.5 Security / Rules

- `si cloudflare waf list|get|update ...`
- `si cloudflare ruleset list|get|deploy|update ...`
- `si cloudflare firewall list|create|delete ...`
- `si cloudflare ratelimit list|create|update|delete ...`

### 6.6 Workers / Pages

- `si cloudflare workers script list|get|deploy|delete ...`
- `si cloudflare workers route list|create|delete ...`
- `si cloudflare workers secret set|delete ...`
- `si cloudflare pages project list|get|create|update|delete ...`
- `si cloudflare pages deploy list|trigger|rollback ...`

### 6.7 Data Platform

- `si cloudflare r2 bucket list|get|create|delete ...`
- `si cloudflare r2 object list|get|put|delete ...`
- `si cloudflare d1 db list|get|create|delete ...`
- `si cloudflare d1 query --db <id> --sql <statement> ...`
- `si cloudflare d1 migration list|apply ...`
- `si cloudflare kv namespace list|create|delete ...`
- `si cloudflare kv key list|get|put|delete|bulk ...`
- `si cloudflare queue list|create|delete ...`

### 6.8 Zero Trust / Tunnel / LB

- `si cloudflare access app list|get|create|update|delete ...`
- `si cloudflare access policy list|create|update|delete ...`
- `si cloudflare tunnel list|get|create|delete ...`
- `si cloudflare tunnel token issue ...`
- `si cloudflare lb list|get|create|update|delete ...`
- `si cloudflare lb pool list|get|create|update|delete ...`

### 6.9 Analytics / Logs / Report

- `si cloudflare analytics http|security|cache ...`
- `si cloudflare logs job create|status|download ...`
- `si cloudflare report <preset> [--from ...] [--to ...] [--json]`

### 6.10 Raw fallback

- `si cloudflare raw --method <GET|POST|PATCH|PUT|DELETE> --path <api-path> [--param ...] [--body ...]`

## 7. Architecture Draft (V1)

Initial design:

1. Product command handlers call SDK service clients directly.
2. Per-command context/auth resolution.
3. Minimal shared formatting and safety utilities.

V1 weaknesses:

- Duplicated context and error handling.
- Inconsistent retry/rate-limit behavior.
- Difficult to keep output/safety consistent across many product areas.

## 8. Architecture Revision (V2, Recommended)

Revised design:

1. Shared `internal/cloudflarebridge` package for:
   - auth token provider
   - request execution
   - retry/backoff/rate-limit handling
   - response normalization
   - error normalization/redaction
2. Unified runtime context resolver:
   - account
   - env label
   - zone
   - base URL
3. Product modules are thin command adapters over shared bridge.
4. Standardized output helpers:
   - human mode (colorized)
   - strict JSON mode
5. Standardized safety helpers for destructive commands.
6. Raw escape hatch command for complete endpoint reach.

## 9. Stack-Wide Enhancement Pass (Second-Pass Revision)

After fitting Cloudflare into the current `si` stack, apply these cross-stack improvements:

1. Shared context engine:
   - unify `stripe`/`github`/`cloudflare` account context resolution patterns in a common helper package.
2. Shared output policy:
   - enforce a single rule: `--json` must produce only JSON across all integrations.
3. Shared redaction library:
   - centralize token/key redaction patterns with provider-specific extensions.
4. Shared safety policy:
   - standard confirmation + non-interactive fail-safe behavior for destructive ops.
5. Shared doctor pattern:
   - `si <provider> doctor` consistency for auth/scope/network diagnostics.
6. Shared audit logging shape:
   - normalize event schema fields (`component`, `event`, `account`, `env`, `request_id`, `status`).

## 10. Global File Boundary Contract

### Allowed paths

- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/settings.go`
- `tools/si/cloudflare*.go` (new/updated)
- `tools/si/*cloudflare*_test.go`
- `tools/si/internal/cloudflarebridge/**` (new)
- `README.md`
- `docs/SETTINGS.md`
- `docs/CLOUDFLARE.md` (new)
- `CHANGELOG.md`
- `tickets/cloudflare-integration-plan.md` (this file)

### Disallowed paths

- `agents/**` (unrelated runtime behavior)
- `tools/codex-init/**`
- `tools/codex-stdout-parser/**`
- unrelated non-cloudflare command behavior (except dispatch/help consistency)

### Secret handling rules

- Never log raw API tokens.
- Never persist decrypted tokens to git-tracked files.
- Redact bearer tokens and sensitive payload fragments in all errors/logs.

## 11. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Not Started |  |  |  | 2026-02-08 |
| WS-01 CLI Entry | Not Started |  |  |  | 2026-02-08 |
| WS-02 Auth/Context/Vault | Not Started |  |  |  | 2026-02-08 |
| WS-03 Bridge Core | Not Started |  |  |  | 2026-02-08 |
| WS-04 Zone + DNS | Not Started |  |  |  | 2026-02-08 |
| WS-05 Security + Rules + TLS/Cache | Not Started |  |  |  | 2026-02-08 |
| WS-06 Workers + Pages | Not Started |  |  |  | 2026-02-08 |
| WS-07 Data Platform (R2/D1/KV/Queues) | Not Started |  |  |  | 2026-02-08 |
| WS-08 Zero Trust + Tunnel + LB | Not Started |  |  |  | 2026-02-08 |
| WS-09 Analytics + Logs + Report + Raw | Not Started |  |  |  | 2026-02-08 |
| WS-10 Testing + E2E | Not Started |  |  |  | 2026-02-08 |
| WS-11 Docs + Release | Not Started |  |  |  | 2026-02-08 |

Status values: `Not Started | In Progress | Blocked | Done`

## 12. Independent Parallel Workstreams

## WS-00 Contracts

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_contract.go`
- `tools/si/internal/cloudflarebridge/types.go`

Deliverables:
1. Runtime context DTO (`account`, `env`, `zone`, `base_url`).
2. Provider interfaces for token and request client.
3. Normalized API error DTO.

Acceptance:
- Other workstreams compile against stable contracts.

## WS-01 CLI Entry

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/cloudflare_cmd.go`

Deliverables:
1. Top-level dispatch for `si cloudflare` (optional alias `si cf`).
2. Command tree usage/help scaffolding.

Acceptance:
- `si --help` and `si cloudflare --help` are accurate.

## WS-02 Auth/Context/Vault

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/settings.go`
- `tools/si/cloudflare_auth.go`
- `tools/si/cloudflare_auth_test.go`

Deliverables:
1. Token resolution hierarchy (flag/settings/env/vault-compatible keys).
2. Context commands (`auth status`, `context list/current/use`, `doctor`).
3. Account/env/zone resolution logic.

Acceptance:
- Clear missing-key messages with exact key names.

## WS-03 Bridge Core

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/cloudflarebridge/client.go`
- `tools/si/internal/cloudflarebridge/errors.go`
- `tools/si/internal/cloudflarebridge/logging.go`
- `tools/si/internal/cloudflarebridge/pagination.go`
- `tools/si/internal/cloudflarebridge/request.go`

Deliverables:
1. HTTP execution wrapper with retry/backoff.
2. Rate-limit handling (`429`, transient `5xx`).
3. Error normalization with redaction.
4. JSON/human response shaping helpers.

Acceptance:
- Deterministic behavior for retries and error output.

## WS-04 Zone + DNS

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_zone_cmd.go`
- `tools/si/cloudflare_dns_cmd.go`
- `tools/si/*cloudflare*_test.go`

Deliverables:
1. Zone list/get/create/update/delete.
2. DNS record list/get/create/update/delete.
3. DNS import/export flows.

Acceptance:
- Common DNS lifecycle works end-to-end.

## WS-05 Security + Rules + TLS/Cache

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_security_cmd.go`
- `tools/si/cloudflare_tls_cmd.go`
- `tools/si/cloudflare_cache_cmd.go`

Deliverables:
1. Rulesets/WAF/firewall/rate-limit essentials.
2. TLS settings and certificate essentials.
3. Cache purge/settings essentials.

Acceptance:
- High-frequency security/performance controls are accessible.

## WS-06 Workers + Pages

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_workers_cmd.go`
- `tools/si/cloudflare_pages_cmd.go`

Deliverables:
1. Workers script/route/secret operations.
2. Pages project/deployment operations.

Acceptance:
- Build/deploy and runtime configuration paths are usable.

## WS-07 Data Platform (R2/D1/KV/Queues)

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_r2_cmd.go`
- `tools/si/cloudflare_d1_cmd.go`
- `tools/si/cloudflare_kv_cmd.go`
- `tools/si/cloudflare_queue_cmd.go`

Deliverables:
1. Common CRUD and operational actions for each service.
2. Safe bulk operations and output controls.

Acceptance:
- Core data-plane tasks can be managed from CLI.

## WS-08 Zero Trust + Tunnel + LB

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_access_cmd.go`
- `tools/si/cloudflare_tunnel_cmd.go`
- `tools/si/cloudflare_lb_cmd.go`

Deliverables:
1. Access app/policy essentials.
2. Tunnel lifecycle + token operations.
3. Load balancer and pool essentials.

Acceptance:
- Common infra edge workflows are covered.

## WS-09 Analytics + Logs + Report + Raw

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/cloudflare_analytics_cmd.go`
- `tools/si/cloudflare_logs_cmd.go`
- `tools/si/cloudflare_report_cmd.go`
- `tools/si/cloudflare_raw_cmd.go`
- `tools/si/cloudflare_output.go`
- `tools/si/cloudflare_safety.go`

Deliverables:
1. Common analytics/reporting presets.
2. Logs job operations.
3. Raw API fallback.
4. Unified output and safety policy.

Acceptance:
- Unsupported endpoints remain reachable without new typed releases.

## WS-10 Testing + E2E

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `tools/si/*cloudflare*_test.go`
- `tools/si/internal/cloudflarebridge/*_test.go`
- `tools/si/testdata/cloudflare/**`

Deliverables:
1. Unit tests for parsers/context/resolution/redaction.
2. Integration tests for bridge request/response behavior.
3. Subprocess E2E tests using mock Cloudflare API servers.
4. Optional live-gated tests (`SI_CLOUDFLARE_E2E=1`).

Acceptance:
- Regressions are caught for command and bridge layers.

## WS-11 Docs + Release

Status:
- State: Not Started
- Owner:
- Notes:

Path ownership:
- `README.md`
- `docs/SETTINGS.md`
- `docs/CLOUDFLARE.md`
- `CHANGELOG.md`

Deliverables:
1. Setup/auth docs with vault-compatible keys.
2. Command recipes for common products.
3. Changelog/release notes.

Acceptance:
- New engineers can configure and use `si cloudflare` quickly.

## 13. Edge Case Matrix (Must Be Tested)

1. Token exists but lacks required scopes for specific product commands.
2. Account alias resolves but account ID is missing.
3. Zone name ambiguous across accounts.
4. Context env maps to missing zone ID.
5. API returns entitlement errors for unavailable products (graceful messaging).
6. Rate limit bursts (`429`) during bulk operations.
7. Eventual consistency after writes (read-after-write delays).
8. Long-running logs job polling and partial failures.
9. Worker script deploy with large payload constraints.
10. D1 query multi-statement failures and partial results.
11. R2 object operations with special characters and large object metadata.
12. Tunnel/access operations requiring additional account-level permissions.
13. Non-interactive destructive commands without `--force`.
14. `--json` mode accidentally mixed with human banners.
15. Base URL overrides (enterprise endpoints) and upload/download host differences.
16. Network/transient TLS errors and retry strategy behavior.
17. Vault key present but decrypt fails (trust drift / recipients mismatch).
18. Concurrent command execution races on shared settings updates.

## 14. Testing Strategy (Deep)

1. Unit tests:
- context resolution
- env key precedence
- parser validation
- redaction coverage
- safety/confirmation behavior

2. Bridge integration tests (mock server):
- auth header wiring
- pagination behavior
- rate-limit retry/backoff
- normalized error extraction

3. Command integration tests:
- representative success/error flows per product command family
- strict `--json` assertions

4. Subprocess E2E tests:
- run `go run ./tools/si cloudflare ...` against mock Cloudflare API server
- validate full flow including context/auth resolution

5. Optional live tests:
- gated behind `SI_CLOUDFLARE_E2E=1`
- limited to safe read-only or explicitly namespaced resources

6. Static analysis:
- `si analyze --module tools/si`
- race-mode test sweep for bridge packages

## 15. Self-Review and Revision (Introspection)

### 15.1 Critique of initial draft

Initial risk areas:

1. Product surface was too broad without explicit parallel boundaries.
2. Environment semantics could be confused with Cloudflare service internals.
3. Auth/security policy needed a stronger default posture.

### 15.2 Revisions applied

1. Added explicit workstream boundaries and file ownership.
2. Locked auth to API token mode for CLI operations.
3. Clarified environment model as context mapping (`prod|staging|dev`) to account/zone defaults.
4. Added strong raw fallback for endpoint parity.
5. Added stack-wide enhancement section to reduce cross-provider drift.

### 15.3 Further enhancements recommended

1. Add `si cloudflare policy check` to preflight command scopes before execution.
2. Add import/export reconciliation for DNS/rules with `plan/apply` semantics.
3. Add audit-tail command for provider-agnostic operational traces.
4. Add idempotency helpers for mutable commands where Cloudflare semantics permit.

## 16. Agent Update Template (Per Workstream)

Use this template for each update:

```md
### WS-XX <Name>
- Status: Not Started | In Progress | Blocked | Done
- Owner:
- Branch:
- PR:
- Changed paths:
- Tests run:
- Open risks/blockers:
- Next step:
- Last updated: YYYY-MM-DD
```

## 17. Out Of Scope (Initial MVP)

1. Full parity with every Cloudflare product endpoint on day one.
2. Automated provisioning of least-privilege API tokens from CLI.
3. Non-token auth modes as first-class runtime paths.
4. Replacing Terraform/IaC entirely.
5. Centralized cloud secret managers as default credential source.

