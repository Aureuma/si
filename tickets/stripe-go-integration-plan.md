# Ticket: `si stripe` Full Stripe Control Integration (Stripe Go Bridge)

Date: 2026-02-07
Owner: Unassigned
Primary Goal: Add `si stripe ...` as a first-class command family for Stripe account control, monitoring, reporting, and broad CRUD operations through the Stripe Go library.

## Implementation Status Snapshot (2026-02-08)

- Overall Status: Done
- Implementation Type: Fully implemented in `tools/si` (bridge, command families, sync, tests, docs)
- Key Commits:
  - `a7294bd` `feat(si): add Stripe bridge and command surface`
  - `dae6900` `feat(si): add structured stripe bridge logging`
  - `a843035` `docs(si): add stripe operator guide and settings reference`
- Notes:
  - Multi-account and `live|sandbox` environment support are implemented.
  - `test` is intentionally not used as a standalone CLI environment mode.

## 1. Requirement Understanding (What Must Be Delivered)

This ticket implements a new command surface:

- `si stripe ...`

It must support:

- Control, monitor, and manage a Stripe account from `si`.
- Reporting workflows.
- Broad CRUD coverage for Stripe objects, including but not limited to product, price, account, organization, and related resources.
- A wrapper/bridge around Stripe Go using our credentials.
- Multi-account operation under one Stripe organization.
- Per-account dual environments: `live` and `sandbox`.
- Explicit environment policy: do not introduce or promote any legacy `test` environment mode in CLI UX; use `sandbox` as the non-production environment.
- Live-to-sandbox replication workflows so sandbox can be refreshed to match live objects (products, prices, coupons, and other supported resources).
- Full Stripe library error visibility to the user (with secret redaction only).
- Output/input UX themed consistently with existing `si` color and interaction conventions.

## 2. Definition Of Done

The implementation is considered complete when all are true:

1. `si stripe` exists in help and dispatch.
2. Credentials and account context are configurable and validated.
3. CRUD works for a broad set of Stripe objects through one consistent CLI interface.
4. Non-CRUD and custom endpoints remain reachable through a generic/raw escape hatch.
5. Reporting commands work and support machine-readable output.
6. Safe defaults exist for mutating commands (confirmation/idempotency/dry-run where applicable).
7. Tests cover parsing, auth/config, request shaping, retries/rate limits, and key command paths.
8. Docs are updated and operationally usable.
9. Multi-account + `live`/`sandbox` environment selection works consistently across all `si stripe` commands.
10. Live-to-sandbox sync has plan/apply modes, drift reporting, and clear object mapping output.
11. Stripe errors are surfaced with full actionable detail (type/code/param/message/request-id/doc URL/status/raw body) while redacting secrets.
12. CLI presentation follows `si` style helpers and color semantics with a machine-readable fallback (`--json`, `--no-color`/`--ansi` behavior aligned with existing CLI).

## 3. Global File Boundary Contract (For Implementation Agent)

### Allowed Paths

- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/settings.go`
- `tools/si/stripe*.go` (new and existing)
- `tools/si/*stripe*_test.go`
- `tools/si/internal/stripebridge/**` (new package tree)
- `README.md`
- `docs/SETTINGS.md`
- `docs/STRIPE.md` (new)
- `tickets/stripe-go-integration-plan.md` (this file updates)

### Disallowed Paths

- `agents/**` (no unrelated agent runtime changes)
- `tools/codex-init/**`
- `tools/codex-stdout-parser/**`
- Existing dyad/codex command behavior unless required for `si stripe` command registration/help wiring only
- Any secret material committed to repository

### Secret Handling Rules

- Never commit API keys or account identifiers that are sensitive.
- Redact secrets in logs/errors (`sk_live_`, `rk_live_`, webhook secrets, etc.).
- Prefer env var references over plaintext key storage in settings.

## 4. Architecture Draft (V1)

Initial architecture idea:

1. Add a typed command layer for common Stripe objects (`product`, `price`, `customer`, `account`, etc.).
2. Route each command to Stripe Go typed service clients.
3. Add reporting subcommands as bespoke handlers.

### V1 Weaknesses

- Stripe surface area is very large; pure typed commands do not scale fast enough.
- Long-tail objects and beta endpoints (including organization-related endpoints) may lag typed support.
- Heavy maintenance burden when Stripe adds/changes resources.

## 5. Architecture Revision (V2, Recommended)

Use a hybrid model:

1. Curated typed UX for high-frequency objects and workflows.
2. Generic object operation bridge for broad CRUD at scale.
3. Raw endpoint passthrough for complete fallback coverage.

### V2 Command Model

- `si stripe auth status`
- `si stripe context list`
- `si stripe context use --account <account-id-or-alias> --env <live|sandbox>`
- `si stripe context current`
- `si stripe object list <object> [flags]`
- `si stripe object get <object> <id> [flags]`
- `si stripe object create <object> [--param k=v ...]`
- `si stripe object update <object> <id> [--param k=v ...]`
- `si stripe object delete <object> <id> [flags]`
- `si stripe sync live-to-sandbox plan [--account ...] [--only ...]`
- `si stripe sync live-to-sandbox apply [--account ...] [--only ...] [--dry-run]`
- `si stripe report <preset> [flags]`
- `si stripe raw --method <GET|POST|DELETE> --path <api path> [--param k=v ...]`

### Why V2

- Meets “full/deep control” requirement while staying maintainable.
- Supports both typed convenience and complete API reach.
- Decouples CLI UX evolution from Stripe object churn.
- Adds explicit account/environment context so commands are safe and predictable.
- Adds reproducible sandbox refresh flows for experimentation and incident rehearsal.

### Environment Model Policy (Mandatory)

1. Organization scope:
- one Stripe organization
- multiple accounts under the organization
2. Environment scope per account:
- `live` (production)
- `sandbox` (non-production)
3. CLI naming policy:
- use `sandbox` terminology in commands/docs/help
- do not add a separate CLI mode named `test`; if Stripe API internals still mention test artifacts, CLI normalizes user-facing language to `sandbox`
4. Safety policy:
- default mutating actions to current selected account+environment
- require explicit confirmation for cross-environment sync apply

## 6. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Done | codex | main |  | 2026-02-07 |
| WS-01 CLI Entry | Done | codex | main |  | 2026-02-07 |
| WS-02 Auth/Config | Done | codex | main |  | 2026-02-07 |
| WS-03 Bridge Core | Done | codex | main |  | 2026-02-07 |
| WS-04 CRUD Registry | Done | codex | main |  | 2026-02-07 |
| WS-05 Object Commands | Done | codex | main |  | 2026-02-07 |
| WS-06 Reporting | Done | codex | main |  | 2026-02-07 |
| WS-07 Safety/Observability | Done | codex | main |  | 2026-02-07 |
| WS-08 Testing | Done | codex | main |  | 2026-02-07 |
| WS-09 Docs/Release | Done | codex | main |  | 2026-02-07 |
| WS-10 Env Sync | Done | codex | main |  | 2026-02-07 |

Status values: `Not Started | In Progress | Blocked | Done`

## 7. Independent Parallel Workstreams

## WS-00 Contracts (Interface-first foundation)

Status:
- State: Done
- Owner: codex
- Notes: Core interfaces and runtime DTOs implemented in `tools/si/stripe_contract.go`.

Path ownership:
- `tools/si/stripe_contract.go`
- `tools/si/stripe_contract_test.go`

Deliverables:
1. Stable interfaces for:
- credential provider
- stripe client wrapper
- object registry lookup
- response formatter
2. DTOs shared by command handlers.

Acceptance:
- All later workstreams compile against these contracts without importing each other directly.

## WS-01 CLI Entry & Dispatch

Status:
- State: Done
- Owner: codex
- Notes: `si stripe` is wired in command dispatch and help text.

Path ownership:
- `tools/si/main.go` (stripe command registration only)
- `tools/si/util.go` (help text only)
- `tools/si/stripe_cmd.go`
- `tools/si/stripe_cmd_test.go`

Deliverables:
1. `si stripe` top-level command with subcommand routing.
2. Coherent usage/help output and error conventions aligned with existing `si` UX.

Acceptance:
- `si --help` and `si stripe --help` show complete and correct usage.

## WS-02 Auth/Config/Credentials

Status:
- State: Done
- Owner: codex
- Notes: Stripe config model, credential precedence, env policy, and context commands implemented.

Path ownership:
- `tools/si/settings.go` (`[stripe]` config block)
- `tools/si/stripe_auth.go`
- `tools/si/stripe_auth_test.go`
- `docs/SETTINGS.md`

Deliverables:
1. Credential resolution precedence:
- CLI flag
- settings
- env (`SI_STRIPE_API_KEY`, optional `SI_STRIPE_ACCOUNT`)
2. `si stripe auth status` command.
3. Secret redaction utilities.
4. Account/environment context model in settings and runtime:
- default account alias/id
- default environment (`live` or `sandbox`)
- per-account credential override support where needed
5. `si stripe context {list|use|current}` commands.

Acceptance:
- Missing/invalid credential errors are actionable and never leak full secrets.
- Environment selection is explicit and never ambiguous across command execution.
- Docs and help clearly state `sandbox` (not a standalone `test` mode in UX).

## WS-03 Stripe Bridge Core (Library wrapper)

Status:
- State: Done
- Owner: codex
- Notes: Bridge implemented with raw request execution, response normalization, pagination helper, and redaction-aware error mapping.

Path ownership:
- `tools/si/internal/stripebridge/client.go`
- `tools/si/internal/stripebridge/request.go`
- `tools/si/internal/stripebridge/pagination.go`
- `tools/si/internal/stripebridge/errors.go`
- `tools/si/internal/stripebridge/client_test.go`

Deliverables:
1. Stripe Go wrapper with:
- request execution
- retries/backoff for transient/rate-limit errors
- pagination support
- context timeouts
2. Common request/response normalization for CLI.
3. Rich Stripe error model mapping:
- preserve HTTP status
- preserve Stripe error fields (`type`, `code`, `decline_code`, `param`, `doc_url`, `request_log_url`, `message`)
- preserve `request-id` response header
- preserve raw error payload for debug output
- redact sensitive tokens/keys only

Acceptance:
- Deterministic behavior under 429/5xx retry scenarios.
- User can see complete actionable failure detail directly in CLI output.

## WS-04 Object Registry & CRUD Capability Map

Status:
- State: Done
- Owner: codex
- Notes: Registry and CRUD operation matrix implemented for broad object support with explicit unsupported-op guidance.

Path ownership:
- `tools/si/internal/stripebridge/registry.go`
- `tools/si/internal/stripebridge/registry_generated.go` (if generated)
- `tools/si/internal/stripebridge/crud.go`
- `tools/si/internal/stripebridge/registry_test.go`

Deliverables:
1. Object registry mapping CLI object names to supported ops and API paths.
2. CRUD operation support matrix per object.
3. Strategy for non-standard delete semantics and nested resources.

Acceptance:
- Unknown/unsupported object-op combinations fail with explicit guidance.

## WS-05 Object Command Handlers

Status:
- State: Done
- Owner: codex
- Notes: Object CRUD and raw handlers implemented; output includes context banner and JSON/human modes.

Path ownership:
- `tools/si/stripe_object_cmd.go`
- `tools/si/stripe_raw_cmd.go`
- `tools/si/stripe_output.go`
- `tools/si/stripe_object_cmd_test.go`

Deliverables:
1. `si stripe object {list|get|create|update|delete}` handlers.
2. `si stripe raw` fallback endpoint handler.
3. Output formats: table/json/raw.
4. Context-aware execution for selected account + environment on every command.

Acceptance:
- CRUD path works across representative object families (products, prices, customers, accounts, etc.).
- Raw mode can reach endpoints beyond registry/typed coverage.
- Command output clearly indicates active account/environment for safety.

## WS-06 Reporting & Monitoring

Status:
- State: Done
- Owner: codex
- Notes: Reporting presets implemented with time-window filters and account/environment context.

Path ownership:
- `tools/si/stripe_report_cmd.go`
- `tools/si/stripe_report_cmd_test.go`
- optional helper files under `tools/si/internal/stripebridge/reporting*.go`

Deliverables:
1. Reporting presets (examples):
- revenue summary
- payment intent status distribution
- subscription churn snapshot
- payout/balance overview
2. Time-window filters and export format support.
3. Account/environment-aware reports and optional cross-account aggregation.

Acceptance:
- Reports are scriptable (`--json`) and human-readable by default.
- Report headers include account/environment context.

## WS-07 Safety, Idempotency, and Observability

Status:
- State: Done
- Owner: codex
- Notes: Confirmation gates, force overrides, idempotency keys, request-id/error surfacing, redaction, and JSONL structured logging are implemented.

Path ownership:
- `tools/si/stripe_safety.go`
- `tools/si/stripe_safety_test.go`
- optional bridge middleware files under `tools/si/internal/stripebridge/`

Deliverables:
1. Mutating command protections:
- confirmation gate for destructive ops
- `--force` override
- idempotency key support
2. Structured logs with redaction.
3. request-id surfacing for support/debug.
4. Error surfacing standard:
- print complete Stripe error context for failures
- include raw payload section when available
- redact secrets only, do not hide actionable fields
5. Output/input theming standard aligned with `si`:
- reuse existing style helpers (`styleHeading`, `styleCmd`, `styleSuccess`, `styleWarn`, `styleError`, `styleDim`)
- honor existing ANSI toggles
- maintain readable prompts/selectors and clean line reset behavior
- color rules for status-like values:
  - `100%`: bold white
  - `<100%` and `>25%`: green
  - `<=25%`: magenta

Acceptance:
- Destructive actions require explicit intent unless forced in non-interactive mode.
- Error diagnostics are detailed enough to debug API/permission issues without reruns.
- Visual output is consistent with existing `si` command family.

## WS-08 Testing Matrix (Unit + Integration)

Status:
- State: Done
- Owner: codex
- Notes: Unit tests now include auth/config, parsing, registry/CRUD, redaction, client request shaping, sync planning, report logic, command-level sync rendering, output-mode tests, and optional live-gated E2E hook.

Path ownership:
- `tools/si/*stripe*_test.go`
- `tools/si/internal/stripebridge/*_test.go`
- `tools/si/testdata/stripe/**`

Deliverables:
1. Unit coverage for parsing, registry resolution, auth fallback, error mapping.
2. Integration tests with mocked Stripe API behavior.
3. Optional gated live tests (`SI_STRIPE_E2E=1`).
4. Multi-account + multi-environment test matrix (`live`, `sandbox`).
5. Replication dry-run/apply tests with partial-failure simulation.
6. Golden tests for colorized and non-colorized output.

Acceptance:
- CI-friendly deterministic tests plus optional live verification path.
- Tests verify full error payload surfacing and redaction behavior.

## WS-09 Docs, Runbook, and Release Notes

Status:
- State: Done
- Owner: codex
- Notes: README, settings reference, STRIPE runbook, and changelog/release note entry are updated.

Path ownership:
- `README.md`
- `docs/STRIPE.md` (new)
- `docs/SETTINGS.md`
- `CHANGELOG.md`

Deliverables:
1. Operator docs with secure credential setup.
2. Command cookbook for CRUD/reporting/raw patterns.
3. Troubleshooting section (auth failures, perms, rate limits, pagination).
4. Environment model docs:
- one org, multiple accounts
- per-account `live` + `sandbox`
- explicit note that CLI uses `sandbox` terminology (no standalone `test` mode UX)
5. Live-to-sandbox sync runbook with safety checklist.

Acceptance:
- A new engineer can run first Stripe command safely without source-diving.
- A new engineer can safely refresh sandbox from live using documented plan/apply flow.

## WS-10 Live-to-Sandbox Sync & Drift Reconciliation

Status:
- State: Done
- Owner: codex
- Notes: live-to-sandbox plan/apply implemented with family filters, dry-run, confirmation, mapping, and partial-failure accumulation.

Path ownership:
- `tools/si/stripe_sync_cmd.go`
- `tools/si/stripe_sync_cmd_test.go`
- `tools/si/internal/stripebridge/sync.go`
- `tools/si/internal/stripebridge/sync_plan.go`
- `tools/si/internal/stripebridge/sync_apply.go`
- `tools/si/internal/stripebridge/sync_test.go`
- `tools/si/testdata/stripe/sync/**`

Deliverables:
1. `si stripe sync live-to-sandbox plan`:
- fetch live and sandbox object inventories
- detect create/update/archive drift
- print plan table and `--json` plan artifact
2. `si stripe sync live-to-sandbox apply`:
- apply ordered sync with id mapping
- support `--only` filters by object family
- support `--dry-run` and confirmation gates
3. Object support targets for first pass:
- products
- prices
- coupons
- promotion codes
- tax rates
- shipping rates
4. Mapping persistence for dependent objects:
- maintain live->sandbox ID map during apply
- resolve references (for example price->product)

Acceptance:
- Sandbox can be refreshed from live with deterministic plan/apply behavior.
- Partial failures report exact object-level errors and continue where safe.
- Re-running apply is idempotent where supported.

## 8. Corner Cases & Risk Register

1. Multiple Stripe accounts / Connect account context mismatch.
2. Restricted API keys missing permissions for certain objects.
3. Environment confusion (`live` vs `sandbox`), including accidental live mutation.
4. Objects without full CRUD semantics.
5. Nested/compound resource paths requiring parent IDs.
6. Pagination over very large datasets.
7. Rate limiting and retry storms.
8. API version drift between account and client defaults.
9. Sensitive payload leakage in logs.
10. Idempotency key collisions on retried mutating requests.
11. Long-running report commands timing out.
12. Organization endpoints not uniformly represented in typed SDK surfaces.
13. Live-to-sandbox sync dependency ordering failures.
14. Sync drift when live changes during apply window.
15. Account context bleed (command runs against wrong account).
16. Terminal UX regressions (prompt/input rendering after interactive cancellation).

Mitigation notes:
- Keep `si stripe raw` for endpoint parity.
- Include explicit permissions errors with object/op names.
- Add dry-run + force gating for destructive operations.
- Always surface Stripe request IDs.
- Require explicit `--env`/context selection or safe defaults with visible context banners.
- Use plan/apply split for sync and lock snapshot IDs where possible.
- Add deterministic terminal output tests (color and non-color modes).

## 9. Integration Sequence (Recommended)

1. WS-00 + WS-01 + WS-02 first (contracts, routing, auth).
2. WS-03 and WS-04 in parallel after contracts stabilize.
3. WS-05 and WS-06 in parallel against bridge/registry.
4. WS-10 sync engine after WS-03/WS-04 contracts are stable.
5. WS-07 hardening pass.
6. WS-08 validation.
7. WS-09 docs/release.

## 10. Agent Update Template (Per Workstream)

Use this block in PR description and in this ticket under each WS:

- Status: Not Started | In Progress | Blocked | Done
- Scope Completed:
- Files Changed:
- Tests Added/Run:
- Open Risks:
- Next Step:

## 11. Explicit Out-Of-Scope For First Cut (Can Be Follow-up Tickets)

1. Webhook receiver/server lifecycle.
2. Real-time streaming dashboards.
3. Multi-tenant RBAC policy engine inside CLI.
4. Automatic code generation pipeline from Stripe OpenAPI in CI (optional enhancement).

## 12. Requested Constraints Audit (User-Specified)

This section explicitly verifies the requested constraints and where each is implemented.

### A) Multi-account + multi-environment model
- Requirement: multiple accounts under one organization; each account has `live` and `sandbox`.
- Status: Implemented.
- Implemented in:
  - `tools/si/settings.go` (`[stripe]` + `[stripe.accounts.<alias>]`)
  - `tools/si/stripe_auth.go` (`auth/context` resolution and selection)
  - `tools/si/stripe_contract.go` (runtime context and client build)
  - `docs/SETTINGS.md` + `docs/STRIPE.md`
- Notes:
  - Organization label is modeled via `stripe.organization`.
  - Account selection supports alias and `acct_...` IDs.
  - Context commands (`list/current/use`) apply account+env consistently.

### B) No standalone `test` mode; use `sandbox`
- Requirement: do not intentionally create/use a separate CLI `test` mode.
- Status: Implemented.
- Implemented in:
  - `tools/si/internal/stripebridge/types.go` (`ParseEnvironment`)
  - `tools/si/stripe_auth.go` (env parsing/validation path)
  - `tools/si/util.go` + docs policy text
  - tests in `tools/si/stripe_auth_test.go`
- Behavior:
  - `test` is rejected with actionable guidance to use `sandbox`.

### C) Live-to-sandbox replication for fresh sandbox parity
- Requirement: replicate live objects (products/prices/coupons/etc.) into sandbox.
- Status: Implemented (first-pass families + mapping + plan/apply).
- Implemented in:
  - `tools/si/stripe_sync_cmd.go`
  - `tools/si/internal/stripebridge/sync.go`
  - `tools/si/internal/stripebridge/sync_plan.go`
  - `tools/si/internal/stripebridge/sync_apply.go`
- Supported first-pass families:
  - `products`, `prices`, `coupons`, `promotion_codes`, `tax_rates`, `shipping_rates`
- Notes:
  - Includes `plan` and `apply` modes, `--dry-run`, `--only`, confirmation, and live→sandbox ID mapping.

### D) Surface Stripe library errors clearly to CLI users
- Requirement: user should see exactly what happened.
- Status: Implemented.
- Implemented in:
  - `tools/si/internal/stripebridge/errors.go` (normalization + redaction)
  - `tools/si/stripe_output.go` (full diagnostic render)
- Surfaced fields:
  - HTTP status, type, code, decline_code, param, message, request_id, doc_url, request_log_url, raw payload
- Notes:
  - Sensitive values are redacted while preserving actionable diagnostics.

### E) SI-consistent color/theme and CLI UX
- Requirement: follow SI color/style conventions and proper themed input/output.
- Status: Implemented.
- Implemented in:
  - `tools/si/stripe_output.go` (style helpers + consistent rendering)
  - `tools/si/stripe_safety.go` (interactive confirmations aligned with SI UX)
  - `tools/si/util.go` (help/theming)
- Notes:
  - Output follows SI style helpers (`styleHeading`, `styleWarn`, `styleError`, `styleDim`, etc.).
  - JSON mode remains machine-readable and can be used with no-color environments.

### F) Broad CRUD coverage including account/organization
- Requirement: deep control across Stripe objects including account/organization and beyond.
- Status: Implemented via hybrid approach.
- Implemented in:
  - `tools/si/internal/stripebridge/registry.go` (curated broad object matrix)
  - `tools/si/stripe_object_cmd.go` (CRUD commands)
  - `tools/si/stripe_object_cmd.go` + `cmdStripeRaw` fallback for full endpoint reach
- Notes:
  - Some resources have API-level nonstandard semantics (for example delete not supported on certain object families); these are explicitly reported with guidance.
  - `si stripe raw` remains the parity escape hatch for full Stripe API coverage.
