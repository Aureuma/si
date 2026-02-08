# Ticket: `si google places` Integration (Vault-Compatible, Multi-Account, Places API New)

Date: 2026-02-08
Owner: Unassigned
Primary Goal: Add a first-class `si google places ...` command family under `si google` for comprehensive Google Places API (New) operations with secure credential handling, consistent `si` UX, and production-safe billing controls.

## Implementation Status Snapshot (2026-02-08)

- Overall Status: Done
- Scope Mode: Implemented
- Notes:
  - Command root added: `si google`
  - Provider subcommand implemented: `si google places`
  - API scope implemented: Places API (New), not Legacy
  - Validation: `go test ./tools/si/...` and `go run ./tools/si analyze --module tools/si` passing

## 0. Decision Lock (Initial)

This plan is explicitly locked to:

1. `si google` command root with one subcommand family now: `si google places`.
2. Google Places API (New) only (`places.googleapis.com`, v1).
3. REST bridge implementation in `tools/si/internal/googleplacesbridge` (custom client), with raw fallback for endpoint parity.
4. API key auth only for the initial implementation.
5. Vault-compatible credentials (`si vault run` compatible key names), with no plaintext secret persistence in git-tracked files.
6. Multi-account and multi-environment context labels (`prod|staging|dev`) mapped to key/project defaults.
7. Field-mask-first command design to control cost/latency and avoid accidental expensive requests.

Rationale:

- Places API (New) requires response field masks in core methods, and billing depends on fields selected.
- API key auth is sufficient for Places Web Service and aligns with current `si` provider model.
- A custom bridge keeps output/error/safety consistency with `si stripe`, `si github`, and `si cloudflare`.
- Raw fallback prevents endpoint lock-in when Google adds new fields/method options.

## 1. Requirement Understanding (What Must Be Delivered)

This ticket introduces:

- `si google places ...`

It must support common Places API (New) workflows comprehensively:

1. Auth/context diagnostics for account/project/key configuration.
2. Autocomplete (New) with session token support.
3. Text Search (New).
4. Nearby Search (New).
5. Place Details (New).
6. Place Photos (New) media retrieval.
7. Place type filtering and discoverability helpers.
8. Session-token lifecycle helpers for autocomplete->details billing flow.
9. Raw API fallback for unsupported/long-tail options.
10. Local operational reporting based on CLI logs (request volume, endpoint mix, estimated SKU mix hints).

Cross-cutting requirements:

- Credentials must come from `si vault` or be fully compatible with `tickets/creds-management-integration-plan.md`.
- Human output must follow `si` styles and color rules.
- `--json` must produce deterministic machine-only JSON output.
- Errors must surface Google API details clearly (`status`, `code`, `message`, request metadata), with secret redaction.
- Command UX should support interactive selection when arguments are omitted and TTY is available.

## 2. Definition Of Done

Implementation is complete when all are true:

1. `si google` is wired in main dispatch/help.
2. `si google places` subcommand tree is wired and documented.
3. Vault-compatible credential and context resolution is implemented.
4. Multi-account + `prod|staging|dev` context mapping works across all places commands.
5. Command families implemented:
   - `auth`, `context`, `doctor`
   - `autocomplete`, `search-text`, `search-nearby`, `details`, `photo`, `types`, `raw`, `report`, `session`
6. Field mask policy is enforced for methods that require it.
7. Session token support is implemented for autocomplete flows (create/reuse/terminate semantics).
8. Error normalization/redaction is consistent.
9. Unit + integration tests cover context, parsing, bridge behavior, and failure paths.
10. Subprocess E2E tests (mock API) validate representative end-to-end command flows.
11. Docs (`README`, `docs/SETTINGS.md`, `docs/GOOGLE_PLACES.md`) are updated and usable.
12. Static analysis passes for `tools/si`.

## 3. API Scope and Canonical Endpoint Mapping

Service endpoint:

- `https://places.googleapis.com`

REST resources/methods (Places API New):

1. `POST /v1/places:autocomplete`
2. `GET /v1/{name=places/*}` (place details)
3. `POST /v1/places:searchText`
4. `POST /v1/places:searchNearby`
5. `GET /v1/{name=places/*/photos/*/media}`

CLI mapping:

1. `si google places autocomplete ...`
2. `si google places details ...`
3. `si google places search-text ...`
4. `si google places search-nearby ...`
5. `si google places photo ...`
6. `si google places raw ...`

Policy lock:

- No Places Legacy endpoint support in `si google places` initial release.

## 4. Vault Compatibility Contract (Mandatory)

Follow `tickets/creds-management-integration-plan.md` principles:

1. Secrets encrypted at rest in vault (`vault/.env.<env>`).
2. No plaintext key persistence in settings/repo files.
3. Runtime decryption/injection compatible with `si vault run -- ...`.

### 4.1 Credential source contract

Primary source:

- Native vault resolver (future).

Compatibility source:

- Environment variables with canonical names (works with `si vault run`).

### 4.2 Canonical secret key names

Global keys:

- `GOOGLE_PLACES_API_KEY`
- `GOOGLE_API_BASE_URL` (optional, default `https://places.googleapis.com`)
- `GOOGLE_DEFAULT_ACCOUNT` (optional)
- `GOOGLE_DEFAULT_ENV` (optional, `prod|staging|dev`)

Per-account keys:

- `GOOGLE_<ACCOUNT>_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_PROJECT_ID`
- `GOOGLE_<ACCOUNT>_DEFAULT_REGION_CODE`
- `GOOGLE_<ACCOUNT>_DEFAULT_LANGUAGE_CODE`
- `GOOGLE_<ACCOUNT>_API_BASE_URL`

Optional per-env account overrides:

- `GOOGLE_<ACCOUNT>_PROD_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_STAGING_PLACES_API_KEY`
- `GOOGLE_<ACCOUNT>_DEV_PLACES_API_KEY`

### 4.3 Settings model (non-secret pointers only)

Add to `settings.toml`:

- `[google]`
  - `default_account`
  - `default_env` (`prod|staging|dev`)
  - `api_base_url`
  - `vault_env`
  - `vault_file`
  - `log_file`
- `[google.accounts.<alias>]`
  - `name`
  - `project_id`
  - `project_id_env`
  - `api_base_url`
  - `vault_prefix`
  - `places_api_key_env`
  - `prod_places_api_key_env`
  - `staging_places_api_key_env`
  - `dev_places_api_key_env`
  - `default_region_code`
  - `default_language_code`

Settings must never store raw API key values.

## 5. Context Model (Multi-Account + Multi-Environment)

### 5.1 Account model

- Multiple GCP account aliases are supported (typically mapped to projects/teams).
- One account can be selected as current default.

### 5.2 Environment model

- `prod|staging|dev` are CLI context labels only.
- Google Places has no native sandbox environment; env labels map to separate keys/projects and defaults.
- This must be explicit in docs/help to avoid confusion.

### 5.3 Request-localization defaults

Context should carry default:

- `languageCode`
- `regionCode`

Commands may override via flags:

- `--language`
- `--region`

## 6. Command Surface (Initial Full Places Scope)

### 6.1 Root

- `si google help`
- `si google places ...`

### 6.2 Auth / Context

- `si google places auth status [--account <alias>] [--env <prod|staging|dev>] [--json]`
- `si google places context list [--json]`
- `si google places context current [--json]`
- `si google places context use --account <alias> [--env <prod|staging|dev>] [--region <cc>] [--language <lc>] [--base-url <url>]`
- `si google places doctor [--account <alias>] [--json]`

### 6.3 Session Token Management

- `si google places session new [--json]`
- `si google places session inspect <token> [--json]`
- `si google places session end <token> [--json]`

Notes:

- Session tokens are user-generated and should be unique per session.
- CLI should provide safe token generation helper (`UUIDv4`) but still allow explicit token input.

### 6.4 Core Search / Retrieval

- `si google places autocomplete --input <text> [--session <token>] [--include-query-predictions] [--location-bias ...] [--region <cc>] [--language <lc>] [--json]`
- `si google places search-text --query <text> [--page-size <n>] [--page-token <token>] [--field-mask <mask>] [--region <cc>] [--language <lc>] [--json]`
- `si google places search-nearby --center <lat,lng> --radius <m> [--included-type <type>] [--excluded-type <type>] [--rank <distance|popularity>] [--field-mask <mask>] [--json]`
- `si google places details <place_id_or_name> [--field-mask <mask>] [--session <token>] [--region <cc>] [--language <lc>] [--json]`
- `si google places photo get <photo_name> [--max-width <px>] [--max-height <px>] [--json]`
- `si google places photo download <photo_name> --output <path> [--max-width <px>] [--max-height <px>]`

### 6.5 Types / Discovery Helpers

- `si google places types list [--group <category>] [--json]`
- `si google places types validate <type> [--json]`

### 6.6 Raw fallback

- `si google places raw --method <GET|POST> --path <api-path> [--param key=value] [--body raw] [--field-mask <mask>] [--json]`

### 6.7 Operational report

- `si google places report usage [--since <ts>] [--until <ts>] [--json]`
- `si google places report sessions [--since <ts>] [--until <ts>] [--json]`

Note:

- Initial reporting is local-log-based, not Cloud Billing API integration.

## 7. Field Mask and Billing Policy (Critical)

1. For `search-text`, `search-nearby`, and `details`, field mask is required by API behavior.
2. CLI should provide:
   - `--field-mask` explicit mode.
   - named presets (`basic`, `discovery`, `contact`, `rating-heavy`, etc.) mapped to explicit fields.
3. CLI should block `*` in non-interactive mode unless `--allow-wildcard-mask` is set.
4. CLI should print a cost-risk hint in human mode when expensive field groups are requested.
5. For autocomplete flows:
   - encourage `--session` reuse from autocomplete to details.
   - warn if details call omits session after autocomplete use.

## 8. Error Model and Output Contract

### 8.1 Error model

Normalize Google errors into provider DTO with:

- HTTP status code
- Google status string (`INVALID_ARGUMENT`, `RESOURCE_EXHAUSTED`, etc.)
- message
- details list when present
- request id metadata if returned
- raw body (redacted)

### 8.2 Output modes

- Human mode: colorized headings + concise summaries.
- `--json`: strict JSON only (no banners/context lines).
- `--raw`: print raw body for debugging.

### 8.3 Redaction

Redact:

- API keys
- bearer tokens (future-proof)
- any secret-like query/header fragments

## 9. Architecture Draft (V1)

Initial design:

1. `cmdGoogle` dispatch + `cmdGooglePlaces` command tree in `tools/si`.
2. Shared runtime context resolver for account/env/key/base-url/default locale.
3. `internal/googleplacesbridge` for request execution/retry/error normalization/logging.
4. Thin command handlers map flags -> API payloads.
5. `raw` subcommand for complete endpoint reach.

V1 weaknesses:

- Potential duplication with other provider bridges.
- Hard to keep field-mask policy centralized if not explicitly abstracted.

## 10. Architecture Revision (V2, Recommended)

Revised design:

1. Keep provider-specific bridge package, but centralize shared concerns:
   - request execution strategy
   - retry classification
   - redaction primitives
   - JSONL event logging shape
2. Add field-mask policy helper package for Places methods:
   - validate masks
   - apply presets
   - detect wildcard
3. Add session-helper module:
   - token generation
   - local session ledger (optional small state file for diagnostics)
4. Enforce command middleware pattern:
   - context resolution
   - json/human mode handling
   - error rendering

## 11. Stack-Wide Enhancement Pass (Cross-Provider)

After fitting `si google places`, apply these stack improvements:

1. Shared provider context core:
   - unify account/env/base-url resolution pattern across stripe/github/cloudflare/google.
2. Shared output contract:
   - guarantee `--json` strictness for all provider commands.
3. Shared redaction helpers:
   - provider extensions + shared default patterns.
4. Shared interactive picker primitives:
   - consistent Esc/Enter cancellation behavior.
5. Shared safety/confirmation policy:
   - destructive op guardrails for all providers.
6. Shared local audit event schema:
   - `component,event,account,env,request_id,status,duration_ms`.

## 12. Global File Boundary Contract

### Allowed paths

- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/settings.go`
- `tools/si/google*.go` (new/updated)
- `tools/si/*google*_test.go`
- `tools/si/internal/googleplacesbridge/**` (new)
- `README.md`
- `docs/SETTINGS.md`
- `docs/GOOGLE_PLACES.md` (new)
- `CHANGELOG.md`
- `tickets/google-places-integration-plan.md` (this file)

### Disallowed paths

- `agents/**` (unrelated runtime behavior)
- `tools/codex-init/**`
- `tools/codex-stdout-parser/**`
- unrelated provider behavior beyond shared helper refinements

### Secret handling rules

- Never log raw API keys.
- Never persist decrypted key values in git-tracked files.
- Redact credentials in error text and logs.

## 13. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Done | Codex | main | N/A | 2026-02-08 |
| WS-01 CLI Entry | Done | Codex | main | N/A | 2026-02-08 |
| WS-02 Auth/Context/Vault | Done | Codex | main | N/A | 2026-02-08 |
| WS-03 Bridge Core | Done | Codex | main | N/A | 2026-02-08 |
| WS-04 FieldMask/Billing Policy | Done | Codex | main | N/A | 2026-02-08 |
| WS-05 Search Commands | Done | Codex | main | N/A | 2026-02-08 |
| WS-06 Details/Photos/Types | Done | Codex | main | N/A | 2026-02-08 |
| WS-07 Session Lifecycle | Done | Codex | main | N/A | 2026-02-08 |
| WS-08 Raw/Output/Errors/Logs | Done | Codex | main | N/A | 2026-02-08 |
| WS-09 Testing + E2E | Done | Codex | main | N/A | 2026-02-08 |
| WS-10 Docs + Release | Done | Codex | main | N/A | 2026-02-08 |

Status values: `Not Started | In Progress | Blocked | Done`

## 14. Independent Parallel Workstreams

## WS-00 Contracts

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_contract.go`
- `tools/si/internal/googleplacesbridge/types.go`

Deliverables:
1. Runtime context DTO.
2. Request/response/error DTOs.
3. Field-mask preset and validation interfaces.

Acceptance:
- Other workstreams compile against stable contracts.

## WS-01 CLI Entry

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/google_cmd.go`

Deliverables:
1. `si google` top-level dispatch.
2. `si google places` usage/help skeleton.

Acceptance:
- `si --help`, `si google --help`, and `si google places --help` are accurate.

## WS-02 Auth/Context/Vault

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/settings.go`
- `tools/si/google_auth.go`
- `tools/si/google_auth_test.go`

Deliverables:
1. Multi-account/env context resolution.
2. Vault-compatible env key lookup.
3. Auth/context/doctor commands.

Acceptance:
- Missing-key and invalid-context errors are actionable and explicit.

## WS-03 Bridge Core

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/googleplacesbridge/client.go`
- `tools/si/internal/googleplacesbridge/errors.go`
- `tools/si/internal/googleplacesbridge/logging.go`

Deliverables:
1. HTTP client wrapper with retry/backoff.
2. Request construction and header policy.
3. Error normalization/redaction.

Acceptance:
- Deterministic behavior under transient errors.

## WS-04 FieldMask/Billing Policy

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_fieldmask.go`
- `tools/si/google_fieldmask_test.go`

Deliverables:
1. Field mask presets.
2. Wildcard safety policy.
3. Field-mask required guard for relevant commands.
4. Billing-risk hint logic.

Acceptance:
- Commands fail fast for invalid/missing masks where required.

## WS-05 Search Commands

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_search_cmd.go`

Deliverables:
1. `autocomplete`
2. `search-text`
3. `search-nearby`

Acceptance:
- Search command matrix functions with pagination/session support.

## WS-06 Details/Photos/Types

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_place_cmd.go`
- `tools/si/google_photo_cmd.go`
- `tools/si/google_types_cmd.go`

Deliverables:
1. `details`
2. `photo get/download`
3. `types list/validate`

Acceptance:
- Typical retrieval and photo download flows succeed with attribution metadata surfaced.

## WS-07 Session Lifecycle

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_session_cmd.go`
- `tools/si/google_session_store.go`

Deliverables:
1. Session token generation helper.
2. Session inspect/end UX.
3. Optional local session ledger for diagnostics.

Acceptance:
- Session behavior supports autocomplete->details workflow and warnings.

## WS-08 Raw/Output/Errors/Logs

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_raw_cmd.go`
- `tools/si/google_output.go`
- `tools/si/google_safety.go`

Deliverables:
1. Raw endpoint execution.
2. Human/JSON/raw output policies.
3. Normalized error printing.
4. JSONL audit logs.

Acceptance:
- Output and error surfaces are consistent with other providers.

## WS-09 Testing + E2E

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/*google*_test.go`
- `tools/si/internal/googleplacesbridge/*_test.go`
- `tools/si/testdata/googleplaces/**`

Deliverables:
1. Unit tests for parsing/context/fieldmask/session.
2. Bridge integration tests (mock server).
3. Subprocess E2E tests for command flows.

Acceptance:
- Regressions are caught before release.

## WS-10 Docs + Release

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `README.md`
- `docs/SETTINGS.md`
- `docs/GOOGLE_PLACES.md`
- `CHANGELOG.md`

Deliverables:
1. Setup/auth docs.
2. Command recipes and field-mask guidance.
3. Release/changelog notes.

Acceptance:
- New engineers can configure and run `si google places` without source diving.

## 15. Edge Case Matrix (Must Be Tested)

1. Missing field mask on methods that require mask.
2. Wildcard field mask in non-interactive mode without explicit override.
3. Invalid `regionCode` or `languageCode`.
4. Invalid or reused session token flows.
5. Session token used across wrong API version assumptions.
6. Pagination token invalid/expired.
7. Photos endpoint without `maxWidthPx`/`maxHeightPx`.
8. Photo name expired/stale from cached value.
9. Quota/rate-limit errors (`RESOURCE_EXHAUSTED`).
10. Key restricted to wrong API or wrong source restriction.
11. API key present but project billing disabled.
12. Multi-account context selection collisions.
13. Non-interactive destructive operations without `--force` where applicable.
14. `--json` output accidentally mixed with banners.
15. EEA-regional behavior differences and localization expectations.

## 16. Testing Strategy (Deep)

1. Unit tests:
- context resolution
- env precedence
- field-mask validation and presets
- session token generation/validation
- redaction behavior

2. Bridge integration tests (mock server):
- header wiring (`X-Goog-Api-Key`, `X-Goog-FieldMask`)
- request/response normalization
- retry/backoff for transient failures
- error DTO parsing

3. Command integration tests:
- representative success/error for each command family
- strict `--json` assertions

4. Subprocess E2E tests:
- `go run ./tools/si google places ...` against mock server
- autocomplete->details with shared session token
- search pagination paths

5. Optional live-gated tests:
- `SI_GOOGLE_PLACES_E2E=1`
- read-only-safe requests on dedicated non-prod key/project

6. Static analysis:
- `si analyze --module tools/si`

## 17. Self-Review and Revision (Introspection)

### 17.1 Initial draft risks

1. Too much command surface without strong policy around field masks and billing.
2. Possible confusion between provider environments and CLI context environments.
3. Risk of under-specifying session token behavior.

### 17.2 Revisions applied

1. Elevated field-mask and billing policy to a dedicated workstream (WS-04).
2. Explicitly documented no native Places sandbox; env labels are CLI mapping only.
3. Added dedicated session lifecycle workstream (WS-07).
4. Added raw fallback and strict JSON contracts for future endpoint change resilience.

### 17.3 Further enhancements recommended

1. Add optional Google Cloud Monitoring/Billing integration for provider-side usage reporting.
2. Add policy preflight command:
   - `si google places policy check`
   - validates key restrictions and required API enablement.
3. Add plan/apply mode for bulk place enrichment workflows.

## 18. Agent Update Template (Per Workstream)

Use this template for implementation updates:

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

## 19. Out Of Scope (Initial Release)

1. Places Legacy API support.
2. Non-Places Google APIs under `si google`.
3. OAuth-based server-to-server flows.
4. Full Cloud Billing API integration for exact cost accounting.
5. AI ranking/relevance post-processing beyond API results.

## 20. Primary Source References

Official Google docs used for this plan (primary sources):

1. Places API (New) REST reference:
   - https://developers.google.com/maps/documentation/places/web-service/reference/rest
2. Text Search (New):
   - https://developers.google.com/maps/documentation/places/web-service/text-search
3. Nearby Search (New):
   - https://developers.google.com/maps/documentation/places/web-service/nearby-search
4. Autocomplete (New):
   - https://developers.google.com/maps/documentation/places/web-service/place-autocomplete
5. Place Details (New):
   - https://developers.google.com/maps/documentation/places/web-service/place-details
6. Place Photos (New):
   - https://developers.google.com/maps/documentation/places/web-service/place-photos
7. Choose fields / field mask guidance:
   - https://developers.google.com/maps/documentation/places/web-service/choose-fields
8. Usage and billing:
   - https://developers.google.com/maps/documentation/places/web-service/usage-and-billing
9. Session tokens:
   - https://developers.google.com/maps/documentation/places/web-service/place-session-tokens
10. API key setup and security guidance:
   - https://developers.google.com/maps/documentation/places/web-service/get-api-key
   - https://developers.google.com/maps/api-security-best-practices
