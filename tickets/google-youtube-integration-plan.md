# Ticket: `si google youtube` Full YouTube Data API Integration (Vault-Compatible, Multi-Account)

Date: 2026-02-08
Owner: Unassigned
Primary Goal: Add `si google youtube ...` as a first-class command family for broad YouTube Data API control, monitoring, and operations using vault-compatible credentials, consistent `si` UX, and production-safe quota/policy controls.

## Implementation Status Snapshot (2026-02-08)

- Overall Status: Implemented
- Scope Mode: Completed in code + tests + docs
- Notes:
  - Command root added: `si google youtube`
  - Bridge package added: `tools/si/internal/youtubebridge`
  - OAuth store/flow added: `tools/si/google_oauth_store.go`, `tools/si/google_oauth_flow.go`
  - Docs added: `docs/GOOGLE_YOUTUBE.md`
  - "Newest" API decision lock retained: YouTube Data API v3 is the current GA API as of 2026-02-08.

## 0. Decision Lock (Updated For Current API Reality)

This plan is explicitly locked to:

1. `si google youtube` as the canonical command path.
2. YouTube Data API v3 (latest GA surface as of 2026-02-08).
3. Direct REST bridge implementation in `tools/si/internal/youtubebridge` (custom client), with raw fallback for endpoint parity.
4. Dual auth modes:
   - API key for public read requests.
   - OAuth 2.0 for user/channel private data and mutations.
5. Service accounts are not supported for YouTube Data API user/channel operations and must be blocked with actionable errors.
6. Multi-account + multi-environment context labels (`prod|staging|dev`) mapped to separate projects/credentials.
7. Quota-aware command policy (cost hints, safe defaults, mutation confirmations).

Rationale:

- The official YouTube API docs and revision history continue to identify v3 as the current API.
- YouTube Data API requires OAuth for write/private operations and supports API keys for public reads.
- Existing `si` provider integrations (stripe/github/cloudflare/google places) use custom bridge patterns for unified output/safety behavior.
- Raw fallback is necessary to keep pace with API changes without waiting for typed command coverage.

## 1. Requirement Understanding (What Must Be Delivered)

This ticket introduces:

- `si google youtube ...`

It must support common YouTube Data API workflows comprehensively:

1. Auth/context diagnostics with clear mode visibility (api-key vs oauth).
2. Public discovery/search/listing (search, videos, channels, playlists).
3. Creator/channel operations (playlist/video metadata management, subscriptions, comments).
4. Upload operations including resumable uploads for videos.
5. Live operations (common live broadcast/stream management paths).
6. Caption and thumbnail operations.
7. Local operational reporting for request volume/quota estimate/error trends.
8. Raw endpoint fallback for uncovered/long-tail methods.

Cross-cutting requirements:

- Credentials must come from `si vault` or be fully compatible with `tickets/creds-management-integration-plan.md`.
- Human output must follow `si` style and color conventions.
- `--json` must be strict JSON only.
- Errors must surface API details (`status`, `reason`, `message`, request id) with secret redaction.
- Interactive selector behavior should match existing `si` command behavior.

## 2. Definition Of Done

Implementation is complete when all are true:

Status: Completed. All 12 criteria below are implemented.

1. `si google youtube` is wired in dispatch/help.
2. Vault-compatible credential and context resolution is implemented.
3. Multi-account + `prod|staging|dev` mapping works across all youtube commands.
4. Auth commands implemented (`auth status`, `auth login`, `auth logout`, `context list/current/use`, `doctor`).
5. Core command families implemented:
   - `search`, `channel`, `video`, `playlist`, `playlist-item`, `subscription`, `comment`, `caption`, `thumbnail`, `live`, `raw`, `report`.
6. Resumable upload flow implemented for `video upload`.
7. Quota-aware policy implemented (estimate, hints, safe defaults).
8. Error normalization/redaction and strict output mode handling implemented.
9. Unit + integration tests cover context/auth parsing, bridge behavior, and failure paths.
10. Subprocess E2E tests validate representative authenticated and unauthenticated flows.
11. Docs are updated (`README`, `docs/SETTINGS.md`, `docs/GOOGLE_YOUTUBE.md`, `CHANGELOG.md`).
12. Static analysis passes for `tools/si`.

## 3. API Scope and Command Mapping

Base endpoint:

- `https://www.googleapis.com/youtube/v3`

Upload endpoint (resumable):

- `https://www.googleapis.com/upload/youtube/v3/videos`

### 3.1 Core Resource Coverage (Common Operations)

1. Search
- API: `search.list`
- CLI: `si google youtube search list ...`

2. Channels
- API: `channels.list`, `channels.update`
- CLI: `si google youtube channel list|get|mine|update ...`

3. Videos
- API: `videos.list`, `videos.update`, `videos.delete`, `videos.insert`, `videos.rate`, `videos.getRating`
- CLI: `si google youtube video list|get|update|delete|upload|rate|get-rating ...`

4. Playlists
- API: `playlists.list|insert|update|delete`
- CLI: `si google youtube playlist list|get|create|update|delete ...`

5. Playlist items
- API: `playlistItems.list|insert|update|delete`
- CLI: `si google youtube playlist-item list|add|update|remove ...`

6. Subscriptions
- API: `subscriptions.list|insert|delete`
- CLI: `si google youtube subscription list|create|delete ...`

7. Comments and comment threads
- API: `comments.list|insert|update|delete`, `commentThreads.list|insert`
- CLI: `si google youtube comment list|get|create|update|delete|thread ...`

8. Captions
- API: `captions.list|insert|update|delete|download`
- CLI: `si google youtube caption list|upload|update|delete|download ...`

9. Thumbnails
- API: `thumbnails.set`
- CLI: `si google youtube thumbnail set ...`

10. Live streaming basics (within Data API family)
- API: `liveBroadcasts.*`, `liveStreams.*`, `liveChatMessages.list|insert|delete`
- CLI: `si google youtube live broadcast|stream|chat ...`

11. Localization/support helpers
- API: `i18nLanguages.list`, `i18nRegions.list`, `videoCategories.list`
- CLI: `si google youtube support languages|regions|categories ...`

12. Raw fallback
- CLI: `si google youtube raw --method ... --path ...`

## 4. Auth Model and Credential Contract

## 4.1 Auth modes

1. API key mode (public reads)
- Suitable for public resources where OAuth is not required.

2. OAuth 2.0 mode (private/user/channel + mutations)
- Required for inserts/updates/deletes and private resource access.

3. Service account mode
- Explicitly unsupported for YouTube user/channel operations.
- CLI must fail fast with clear guidance.

## 4.2 Vault-compatible credential keys

Global keys:

- `GOOGLE_API_BASE_URL` (optional)
- `GOOGLE_DEFAULT_ACCOUNT` (optional)
- `GOOGLE_DEFAULT_ENV` (optional; `prod|staging|dev`)

Per-account YouTube keys:

- `GOOGLE_<ACCOUNT>_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_ID`
- `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_SECRET`
- `GOOGLE_<ACCOUNT>_YOUTUBE_REDIRECT_URI` (optional)
- `GOOGLE_<ACCOUNT>_YOUTUBE_REFRESH_TOKEN` (optional, for headless)
- `GOOGLE_<ACCOUNT>_YOUTUBE_ACCESS_TOKEN` (optional override)

Per-account per-env key overrides:

- `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_API_KEY`
- `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_REFRESH_TOKEN`
- `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_REFRESH_TOKEN`
- `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_REFRESH_TOKEN`

## 4.3 Settings model (no raw secrets)

Extend `settings.toml` in a Google service-aware way:

- `[google]`
  - `default_account`
  - `default_env`
  - `api_base_url`
  - `vault_env`
  - `vault_file`
  - `log_file`

- `[google.youtube]`
  - `default_auth_mode` (`api-key|oauth`, default `api-key`)
  - `upload_chunk_size_mb`
  - `quota_budget_daily`

- `[google.youtube.accounts.<alias>]`
  - `name`
  - `project_id`
  - `project_id_env`
  - `vault_prefix`
  - `youtube_api_key_env`
  - `prod_youtube_api_key_env`
  - `staging_youtube_api_key_env`
  - `dev_youtube_api_key_env`
  - `youtube_client_id_env`
  - `youtube_client_secret_env`
  - `youtube_redirect_uri_env`
  - `youtube_refresh_token_env`
  - `default_region_code`
  - `default_language_code`

Settings must never persist raw key/secret/token values in git-tracked files.

## 5. Multi-Account and Multi-Environment Model

1. One organization, multiple Google account aliases.
2. Each alias can map to separate prod/staging/dev projects and credentials.
3. `test` environment is intentionally not supported.
4. YouTube has no native sandbox environment; `staging|dev` map to separate GCP projects/channels used for non-prod workflows.

Context commands:

- `si google youtube context list`
- `si google youtube context current`
- `si google youtube context use --account <alias> --env <prod|staging|dev>`

## 6. Command Surface (Planned)

## 6.1 Root

- `si google youtube help`

## 6.2 Auth and context

- `si google youtube auth status [--account <alias>] [--env <prod|staging|dev>] [--mode <api-key|oauth>] [--json]`
- `si google youtube auth login [--account <alias>] [--env <prod|staging|dev>] [--scopes <csv>] [--device] [--json]`
- `si google youtube auth logout [--account <alias>] [--env <prod|staging|dev>] [--json]`
- `si google youtube context list|current|use ...`
- `si google youtube doctor [--account <alias>] [--env <prod|staging|dev>] [--json]`

## 6.3 Search/discovery

- `si google youtube search list --query <text> [--type <video|channel|playlist>] [--max-results <n>] [--page-token <t>] [--json]`
- `si google youtube support languages|regions|categories ...`

## 6.4 Channel/video/playlist lifecycle

- `si google youtube channel list|get|mine|update ...`
- `si google youtube video list|get|update|delete ...`
- `si google youtube video upload --file <path> [--title ...] [--description ...] [--privacy <public|unlisted|private>] [--resumable]`
- `si google youtube video rate|get-rating ...`
- `si google youtube playlist list|get|create|update|delete ...`
- `si google youtube playlist-item list|add|update|remove ...`

## 6.5 Audience interaction

- `si google youtube subscription list|create|delete ...`
- `si google youtube comment list|get|create|update|delete ...`
- `si google youtube comment thread list|create ...`

## 6.6 Asset ops

- `si google youtube caption list|upload|update|delete|download ...`
- `si google youtube thumbnail set --video-id <id> --file <path> ...`

## 6.7 Live operations (common path)

- `si google youtube live broadcast list|get|create|update|bind|transition|delete ...`
- `si google youtube live stream list|get|create|update|delete ...`
- `si google youtube live chat list|send|delete ...`

## 6.8 Raw/report

- `si google youtube raw --method <GET|POST|PUT|DELETE> --path <api-path> [--param key=value] [--body raw] [--json]`
- `si google youtube report usage [--since <ts>] [--until <ts>] [--json]`
- `si google youtube report quota [--estimate] [--json]`

## 7. Quota, Performance, and Safety Policy

1. Quota estimator:
- Estimate request cost by method and pagination fan-out before execution (`--estimate` / human hint).

2. Safe defaults:
- Conservative `maxResults` defaults.
- Disable expensive fan-out by default unless `--all` or explicit pagination loop is requested.

3. Mutation safety:
- `--force` confirmation for destructive operations.
- Optional `--if-match-etag` support for safe update/delete concurrency.

4. Update safety for `part`:
- Explicit warnings that `part` controls mutable fields and omitted mutable fields can be cleared.
- Dry-run preview for update payload.

5. Upload resilience:
- Resumable upload only for large files by default.
- Retry/backoff + resume checkpointing.

6. Compression/caching:
- Enable gzip and ETag-aware conditional reads for list/get where possible.

## 8. Error Model and Output Contract

## 8.1 Error normalization

Normalize errors to provider DTO with:

- HTTP status code
- API error reason/type
- message
- request id (if available)
- raw redacted body
- actionable fix hints (scope missing, quota, unlinked channel, auth refresh)

## 8.2 Output modes

- Human mode: colorized headings and concise summaries.
- `--json`: strict JSON only.
- `--raw`: raw body passthrough for debugging.

## 8.3 Redaction

Redact:

- API keys
- access/refresh tokens
- OAuth client secrets
- sensitive headers/query strings

## 9. Architecture Draft (V1)

1. `cmdGoogleYoutube` dispatch under `si google`.
2. Runtime context resolver (account/env/auth-mode/region/language/base-url).
3. `internal/youtubebridge` for request execution/retry/error/logging.
4. Typed command handlers for common resources.
5. Raw fallback for parity.

V1 weaknesses:

- Potential duplication across Google providers (places and youtube).
- OAuth token lifecycle complexity can leak into command handlers if not centralized.

## 10. Architecture Revision (V2, Recommended)

1. Introduce Google provider core shared by Places + YouTube:
- shared context resolution
- shared redaction/log schema
- shared auth/token interfaces

2. Central OAuth token manager for Google services:
- token acquisition/refresh
- token storage policy (keyring/file fallback)
- expiry/invalid_grant handling

3. Keep service-specific bridges:
- `googleplacesbridge`
- `youtubebridge`

4. Add command middleware pattern:
- resolve context
- inject client
- enforce output mode
- normalize errors

5. Add schema-awareness support:
- optionally use discovery metadata snapshots for method/parameter validation drift checks.

## 11. Stack-Wide Enhancement Pass (Cross-Provider)

After adding `si google youtube`, apply these stack refinements:

1. Shared provider runtime contract:
- unify `auth status`, `context`, `doctor`, `raw`, `report` UX across stripe/github/cloudflare/google places/google youtube.

2. Shared strict-json guard:
- test helper that fails any provider command if banners leak into `--json` output.

3. Shared retry/backoff profile:
- standardized safe-method retry policy and jitter.

4. Shared operation logs:
- JSONL schema fields: `component,event,account,env,method,path,status,request_id,duration_ms`.

5. Shared interactive selector primitives:
- ensure Esc/Enter cancellation consistency for all interactive provider flows.

6. Shared docs taxonomy:
- one provider guide per integration and one central settings reference section template.

## 12. Global File Boundary Contract

### Allowed paths

- `tools/si/main.go`
- `tools/si/util.go`
- `tools/si/settings.go`
- `tools/si/google*.go` (new/updated)
- `tools/si/*google*youtube*_test.go`
- `tools/si/internal/youtubebridge/**` (new)
- `README.md`
- `docs/SETTINGS.md`
- `docs/GOOGLE_YOUTUBE.md` (new)
- `CHANGELOG.md`
- `tickets/google-youtube-integration-plan.md` (this file)

### Disallowed paths

- `agents/**` (unrelated runtime behavior)
- `tools/codex-init/**`
- `tools/codex-stdout-parser/**`
- unrelated provider behavior beyond shared helper convergence

### Secret handling rules

- Never log raw secret material.
- Never persist decrypted secrets in git-tracked files.
- Keep refresh tokens in secure local storage or vault-injected runtime env.

## 13. Workstream Status Board

| Workstream | Status | Owner | Branch | PR | Last Update |
|---|---|---|---|---|---|
| WS-00 Contracts | Done | codex | main | n/a | 2026-02-08 |
| WS-01 CLI Entry | Done | codex | main | n/a | 2026-02-08 |
| WS-02 Auth/Context/Vault | Done | codex | main | n/a | 2026-02-08 |
| WS-03 OAuth Token Manager | Done | codex | main | n/a | 2026-02-08 |
| WS-04 Bridge Core | Done | codex | main | n/a | 2026-02-08 |
| WS-05 Search/Read Commands | Done | codex | main | n/a | 2026-02-08 |
| WS-06 Mutation/Upload Commands | Done | codex | main | n/a | 2026-02-08 |
| WS-07 Live/Captions/Assets | Done | codex | main | n/a | 2026-02-08 |
| WS-08 Raw/Output/Safety/Quota | Done | codex | main | n/a | 2026-02-08 |
| WS-09 Testing + E2E | Done | codex | main | n/a | 2026-02-08 |
| WS-10 Docs + Release | Done | codex | main | n/a | 2026-02-08 |

Status values: `Not Started | In Progress | Blocked | Done`

## 14. Independent Parallel Workstreams

## WS-00 Contracts

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_youtube_contract.go`
- `tools/si/internal/youtubebridge/types.go`

Deliverables:
1. Runtime context DTO.
2. Request/response/error DTOs.
3. Quota metadata DTO.

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
- `tools/si/google_youtube_cmd.go`

Deliverables:
1. `si google youtube` dispatch wiring.
2. Help text and usage trees.

Acceptance:
- `si --help`, `si google --help`, and `si google youtube --help` are accurate.

## WS-02 Auth/Context/Vault

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/settings.go`
- `tools/si/google_youtube_auth.go`
- `tools/si/google_youtube_auth_test.go`

Deliverables:
1. Account/env/auth-mode resolution.
2. Vault-compatible key resolution.
3. `auth status`, `context`, `doctor` commands.

Acceptance:
- Missing-key and invalid-context errors are explicit and actionable.

## WS-03 OAuth Token Manager

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_oauth_store.go`
- `tools/si/google_oauth_flow.go`
- `tools/si/google_oauth_test.go`

Deliverables:
1. OAuth login flow for CLI (installed app/device flow).
2. Token refresh and expiry handling.
3. Secure local token storage strategy (keyring/file fallback).

Acceptance:
- OAuth workflows are reliable for long-lived CLI use.

## WS-04 Bridge Core

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/internal/youtubebridge/client.go`
- `tools/si/internal/youtubebridge/errors.go`
- `tools/si/internal/youtubebridge/logging.go`

Deliverables:
1. HTTP wrapper with retries/backoff.
2. Auth header/query policy by mode.
3. Error normalization/redaction.

Acceptance:
- Deterministic behavior under transient failures.

## WS-05 Search/Read Commands

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_youtube_read_cmd.go`

Deliverables:
1. search, channel get/list/mine, video list/get.
2. playlist and playlist-item list/get.
3. subscription/comment read paths.

Acceptance:
- Common read workflows function with pagination and fields/part control.

## WS-06 Mutation/Upload Commands

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_youtube_write_cmd.go`
- `tools/si/google_youtube_upload_cmd.go`

Deliverables:
1. video/playlist/playlist-item/subscription/comment CRUD operations.
2. resumable upload implementation.
3. mutation safety prompts and dry-run previews.

Acceptance:
- Common mutation and upload workflows are robust and safe.

## WS-07 Live/Captions/Assets

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_youtube_live_cmd.go`
- `tools/si/google_youtube_caption_cmd.go`
- `tools/si/google_youtube_thumbnail_cmd.go`

Deliverables:
1. live broadcast/stream/chat common operations.
2. caption lifecycle operations.
3. thumbnail set operation.

Acceptance:
- Typical creator live/media management paths are covered.

## WS-08 Raw/Output/Safety/Quota

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/google_youtube_raw_cmd.go`
- `tools/si/google_youtube_output.go`
- `tools/si/google_youtube_safety.go`
- `tools/si/google_youtube_report_cmd.go`

Deliverables:
1. raw endpoint execution.
2. strict output mode handling.
3. safety confirmations for destructive operations.
4. local usage/quota estimate reporting.

Acceptance:
- Output and safety behavior matches other `si` provider commands.

## WS-09 Testing + E2E

Status:
- State: Done
- Owner:
- Notes:

Path ownership:
- `tools/si/*google*youtube*_test.go`
- `tools/si/internal/youtubebridge/*_test.go`
- `tools/si/testdata/youtube/**`

Deliverables:
1. Unit tests for context/auth/scope/quota logic.
2. Bridge integration tests with mock server.
3. Subprocess E2E tests for representative flows.
4. Optional live-gated tests (`SI_YOUTUBE_E2E=1`).

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
- `docs/GOOGLE_YOUTUBE.md`
- `CHANGELOG.md`

Deliverables:
1. Setup/auth docs and key naming guide.
2. Command recipes for common workflows.
3. Release notes.

Acceptance:
- Engineers can configure and run `si google youtube` without source diving.

## 15. Edge Case Matrix (Must Be Tested)

1. API key used for a mutation/private method returns auth errors.
2. OAuth token expired/invalid_grant refresh flow.
3. Service account credentials provided by mistake.
4. `youtubeSignupRequired` / unlinked account behavior.
5. Missing/insufficient scopes for requested operation.
6. Quota exceeded (`quotaExceeded`) and daily budget handling.
7. Pagination token invalid/expired.
8. `part` update accidentally clearing mutable fields.
9. Large upload interruption + resume from checkpoint.
10. Captions `sync` parameter assumptions (deprecated behavior).
11. Live operation attempted on non-live-enabled channel.
12. Concurrent mutation conflicts without ETag protection.
13. Strict JSON mode accidentally includes banners/warnings.
14. Secrets leak via logs/errors/raw output.

## 16. Testing Strategy (Deep)

1. Unit tests:
- account/env/auth-mode resolution
- token source precedence
- scope requirement matrix
- quota estimate logic
- redaction rules

2. Bridge integration tests (mock server):
- auth header/query handling by mode
- retry/backoff behavior
- pagination handling
- normalized error parsing

3. Command integration tests:
- success/error paths for each command family
- strict `--json` assertions

4. Subprocess E2E tests:
- `go run ./tools/si google youtube ...` against mock API server
- OAuth-required flow simulation
- resumable upload flow simulation

5. Optional live-gated tests:
- `SI_YOUTUBE_E2E=1`
- read-safe operations only by default

6. Static analysis:
- `si analyze --module tools/si`

## 17. Self-Review and Revision (Introspection)

### 17.1 Initial draft risks

1. Too broad command surface can delay delivery if done monolithically.
2. OAuth complexity can become brittle without dedicated token manager workstream.
3. Quota surprises if cost and pagination fan-out are not surfaced.

### 17.2 Revisions applied

1. Added explicit WS-03 OAuth token manager boundary.
2. Added quota/safety as dedicated WS-08 workstream.
3. Locked a phased but parallelizable architecture with raw fallback for parity.
4. Added stack-wide enhancement section for provider consistency.

### 17.3 Further enhancements recommended

1. Optional YouTube Analytics/Reporting integration as separate command family (`si google yt-analytics ...`) after Data API baseline.
2. Optional discovery sync tooling for drift detection against Google discovery metadata.
3. Optional policy audit helper command for developer compliance readiness.

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

1. YouTube Analytics API and YouTube Reporting API command surfaces.
2. CMS/content-owner-only advanced partner workflows beyond common paths.
3. Full policy/compliance automation engine.
4. Non-YouTube Google APIs under `si google`.

## 20. Primary Source References

Official sources used to ground this plan:

1. YouTube Data API reference:
- https://developers.google.com/youtube/v3/docs

2. YouTube Data API revision history:
- https://developers.google.com/youtube/v3/revision_history

3. YouTube Data API quota calculator:
- https://developers.google.com/youtube/v3/determine_quota_cost

4. YouTube Data API overview/getting started (part, fields, ETag/gzip notes):
- https://developers.google.com/youtube/v3/getting-started

5. OAuth authorization guide (includes service-account caveat for YouTube):
- https://developers.google.com/youtube/v3/guides/authentication

6. OAuth device flow guide and scope set:
- https://developers.google.com/youtube/v3/guides/auth/devices

7. Resumable upload protocol guide:
- https://developers.google.com/youtube/v3/guides/using_resumable_upload_protocol

8. Error catalog:
- https://developers.google.com/youtube/v3/docs/errors

9. Partial response and `part` behavior:
- https://developers.google.com/youtube/v3/guides/implementation/partial
- https://developers.google.com/youtube/v3/docs/videos/update

10. Pagination guide:
- https://developers.google.com/youtube/v3/guides/implementation/pagination

11. Developer policies/compliance guide:
- https://developers.google.com/youtube/terms/developer-policies-guide

12. Discovery API reference/guides (for optional schema-driven tooling):
- https://developers.google.com/discovery/v1/using
- https://developers.google.com/discovery/v1/reference/apis
