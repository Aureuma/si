# Provider Traffic Policy

`si` now uses a two-part traffic policy for external APIs:

1. Built-in provider defaults in Go (`tools/si/internal/providers/specs.go`)
2. Runtime feedback from API responses (`429`, `Retry-After`, `X-RateLimit-*`, etc.)

There is no external rate-limit or quota reference file to maintain.

## Shared Runtime
- HTTP integrations now execute through a shared runtime (`tools/si/internal/integrationruntime/http_exec.go`).
- Provider-specific code only supplies hooks for:
  - request building + auth injection
  - response normalization
  - provider error mapping
  - retry decision criteria
- The runtime owns admission, retry/backoff, runtime feedback, and response caching.

## How it works
- Every provider call goes through admission (`providers.Acquire`) using a baseline token bucket.
- Every response feeds back into policy (`providers.FeedbackWithLatency`).
- On throttling signals, calls are cooled down until reset/retry windows.
- On rate-limit headers, limiter pace is adapted dynamically.

## Operational usage
- Inspect active provider defaults:
  - `si providers characteristics --json`
- Inspect runtime traffic/health telemetry:
  - `si integrations health --json`
  - includes API version review warnings/errors
- Inspect API version policy coverage:
  - `si providers health --json`
  - includes `version_missing` and `version_invalid` fields
- Run public connectivity probes:
  - `si github doctor --public`
  - `si cloudflare doctor --public`
  - `si google places doctor --public`
  - `si google youtube doctor --public`
  - `si stripe doctor --public`

## CI
- `si-tests` workflow runs full Go tests for `tools/si`.
- `si-live-smoke` workflow runs public probes plus optional authenticated smoke checks gated by repository secrets.
