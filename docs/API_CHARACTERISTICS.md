# Provider Traffic Policy

`si` now uses a two-part traffic policy for external APIs:

1. Built-in provider defaults in the Rust provider catalog/runtime
2. Runtime feedback from API responses (`429`, `Retry-After`, `X-RateLimit-*`, etc.)

There is no external rate-limit or quota reference file to maintain.

## Shared Runtime
- HTTP integrations now execute through the shared Rust runtime.
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
  - `si orbit list --json`
- Inspect runtime traffic/health telemetry:
  - `si integrations health --json`
  - includes API version review warnings/errors
- Inspect API version policy coverage:
  - `si orbit list --json`
  - includes `version_missing` and `version_invalid` fields
- Run public connectivity probes:
  - `si orbit github doctor --public`
  - `si orbit cloudflare doctor --public`
  - `si orbit google places doctor --public`
  - `si orbit google play doctor --public`
  - `si orbit google youtube doctor --public`
  - `si orbit stripe doctor --public`

## CI
- `si-tests` workflow runs Rust workspace checks.
- `si-live-smoke` workflow runs public probes plus optional authenticated smoke checks gated by repository secrets.
