# CLI Direct HTTP Migration (Move Remaining net/http Call Sites to apibridge)

Status: `in progress`

## Goal
Remove remaining direct `net/http` request construction and bespoke URL helpers in CLI command codepaths, so all external HTTP requests go through `tools/si/internal/apibridge` (or a first-class `internal/*bridge` wrapper when appropriate).

This is a follow-on to `tickets/api-bridge-refactor-plan.md` (which is complete).

## Scope (From "A" List)
- `tools/si/google_youtube_resource_cmd.go`: uploads/downloads (resumable + multipart + media + download).
- `tools/si/google_places_resource_cmd.go`: photo/media download path.
- `tools/si/google_oauth_flow.go`: device flow + token exchange + refresh.
- `tools/si/google_youtube_auth.go`: revoke token call.
- `tools/si/codex_profile_status.go`: refresh + usage calls.

## Workstreams

### WS-0: apibridge Prereqs (Streaming + Non-Replayable Bodies)
Status: `done`

Tasks
- Add support for non-string request bodies:
  - byte slice body (avoid string copies)
  - streaming body via `io.Reader`/`io.ReadCloser`
- Add a safe retry rule for non-replayable bodies:
  - if retries are enabled and the body cannot be replayed, fail early or disable retries automatically
  - optionally allow `BodyFactory` to create a fresh reader per attempt
- Add tests covering:
  - request body precedence (raw/json/bytes/stream)
  - body replay behavior across retries

Notes
- For large uploads we may set `MaxRetries=0` to keep behavior predictable unless a replay factory exists.

### WS-1: Google Places CLI Download Path -> apibridge
Status: `pending`

Tasks
- Replace direct `http.NewRequestWithContext` + `http.Client.Do` in the download helper with apibridge client calls.
- Preserve:
  - `X-Goog-Api-Key` header
  - timeouts
  - error normalization via `googleplacesbridge.NormalizeHTTPError`
- Add/extend unit tests (httptest) for:
  - header wiring
  - non-2xx error handling path

### WS-2: YouTube CLI Upload/Download Paths -> apibridge
Status: `pending`

Tasks
- Replace direct net/http flows in:
  - resumable upload init + PUT upload
  - multipart upload
  - media upload
  - media download
- Preserve:
  - upload base URL selection
  - auth application (API key query or OAuth bearer)
  - timeouts for large ops (minutes)
  - error normalization via `youtubebridge.NormalizeHTTPError`
- Remove/retire bespoke `resolveGoogleYouTubeURL` helper if no longer needed.
- Add/extend tests (httptest) for:
  - upload base URL routing
  - auth headers/query injection
  - retry disabled for non-replayable bodies unless explicitly supported

### WS-3: Google OAuth Device Flow -> apibridge
Status: `pending`

Tasks
- Replace `http.NewRequestWithContext`/`Do` for:
  - device code request
  - token exchange
  - refresh token exchange
- Preserve polling semantics (`authorization_pending`, `slow_down`, etc).
- Add tests with httptest server to cover:
  - non-2xx bodies -> error
  - slow_down backoff logic

### WS-4: YouTube Auth Revoke -> apibridge
Status: `pending`

Tasks
- Replace revoke call to use apibridge.
- Ensure error output remains user-friendly.

### WS-5: Codex Profile Status HTTP Calls -> apibridge
Status: `pending`

Tasks
- Replace direct HTTP calls with apibridge.
- Ensure any tokens/credentials are not logged (use `Redact` + `SanitizeURL`).
- Add unit tests where feasible.

### WS-6: Cleanup + Verification
Status: `pending`

Tasks
- Ensure no remaining `http.NewRequestWithContext` / `http.Client.Do` in CLI codepaths for the scoped files.
- Run:
  - `./tools/test.sh`
  - `./tools/test-install-si.sh`
  - `./si analyze --module tools/si`

## Current Status Snapshot
- Completed earlier:
  - Provider bridge migrations (Cloudflare/GitHub/Google Places/YouTube) to apibridge.
  - Provider spec registry.
  - Provider retries honoring `Retry-After`.
  - GitHub App auth provider now uses apibridge.
- Remaining work: WS-0..WS-6 above.
