# API Bridge Refactor (Shared HTTP Engine + Provider Specs)

Status: `done`

## Goal
Reduce duplication across `tools/si/internal/*bridge` clients by extracting a single shared HTTP execution engine (auth/header injection, URL building, retries/backoff, logging, and response capture) while keeping CLI behavior stable.

This is explicitly **not** a “unified API” layer (Merge-style canonical models). It is an operator/automation CLI foundation:
- fast coverage via `raw` + thin typed commands
- consistent auth/context UX
- safe defaults (redaction, logs, retry behavior)
- low maintenance to add more providers

## Non-Goals (For This Ticket)
- Introducing canonical cross-provider schemas.
- Generating provider clients primarily from OpenAPI.
- Changing CLI command surfaces or output formats (beyond internal log details).

## Migration Path (Execution Order)
1. Extract shared engine (`internal/apibridge`) with tests; no CLI behavior changes.
2. Migrate one provider (Cloudflare) to validate the interface.
3. Migrate remaining HTTP-based providers (GitHub, Google Places, YouTube).
4. Add a provider spec registry and move defaults (base URLs, UA, accept headers, request-id headers) out of per-provider code.

## Workstreams

### WS-1: Shared HTTP Engine (`tools/si/internal/apibridge`) (Step 1)
Status: `done`

Tasks
- Add `apibridge.Client` with:
  - portable URL resolution (`baseURL + path + params`, plus absolute URL support)
  - request body support (`RawBody` or `JSONBody`)
  - retries/backoff w/ provider hook for retry decisions
  - structured JSONL logging hooks (request/response/error) w/ shared context
  - capture `status`, `headers`, `body`, `request_id`, `duration_ms`
- Add shared `EventLogger` + JSONL implementation (replaces duplicated JSONL logger copies).
- Unit tests:
  - URL resolution (relative/absolute, query param merge, trimming)
  - request body selection (raw vs JSON)
  - retry policy plumbing (attempt loop + backoff selection)
  - JSONL logger (writes one JSON object per line, creates dirs with correct perms)

Notes
- The engine must not interpret provider-specific envelopes (e.g. Cloudflare `success=false`).
- Provider code remains responsible for parsing payload shapes and turning failures into `APIErrorDetails`.

### WS-2: Migrate Cloudflare Bridge to `apibridge` (Step 2)
Status: `done`

Tasks
- Keep external types stable in `internal/cloudflarebridge` (`ClientConfig`, `Request`, `Response`, errors).
- Replace the bespoke HTTP loop with `apibridge.Client`.
- Preserve:
  - default headers (`Accept`, `User-Agent`, `Authorization`)
  - retry semantics (safe-method + status-code based)
  - response normalization and error parsing
- Update tests (or add tests) to ensure the same behavior on:
  - request header wiring
  - retry-on-429/5xx
  - response parsing of `result`, `errors`, request id extraction

### WS-3: Migrate GitHub / Google Places / YouTube Bridges (Step 3)
Status: `done`

Tasks
- GitHub:
  - token retrieval per attempt via a `PrepareRequest` hook (needs owner/repo/installation context)
  - preserve special retry logic (403 secondary-rate-limit / abuse detection)
- Google Places:
  - preserve `X-Goog-Api-Key` + `X-Goog-FieldMask`
  - preserve list extraction + `ListAll` pagination helper
- YouTube:
  - preserve dual base URLs (normal vs upload)
  - preserve API key query injection in api-key mode, OAuth bearer injection in oauth mode
  - preserve list extraction + `ListAll` pagination helper

Verification
- All existing unit + e2e subprocess tests continue passing.
- `./si analyze --module tools/si` passes.

### WS-4: Provider Spec Registry (Step 4)
Status: `done`

Tasks
- Add `tools/si/internal/providers/specs.go` (or similar) with:
  - default base URL(s)
  - default `User-Agent`
  - default `Accept`
  - request-id header(s) used for response correlation
  - default retry count/timeouts (if appropriate)
- Update each `NewClient` to pull defaults from the spec registry instead of hardcoded strings.

Notes
- Specs are defaults only; settings/env/flags still override.
- The goal is to reduce duplicated constants and make “new provider” additions predictable.

### WS-5: End-to-End Verification
Status: `done`

Tasks
- Run:
  - `./tools/test.sh`
  - `./tools/test-install-si.sh`
  - `./si analyze --module tools/si`
- Spot-check a couple of local `--help` outputs to ensure no dispatch regression.

Results
- `./tools/test.sh` passed.
- `./tools/test-install-si.sh` passed.
- `./si analyze --module tools/si` passed (`go vet` + `golangci-lint`).

## Risks / Mitigations
- Risk: changing retry semantics silently.
  - Mitigation: keep provider-specific retry functions; add explicit tests around retry conditions.
- Risk: logging/redaction regressions.
  - Mitigation: keep provider redaction logic where it exists; only centralize JSONL writing.
- Risk: breaking YouTube upload URL handling.
  - Mitigation: keep URL selection logic in youtubebridge; pass absolute URLs into the shared engine.
