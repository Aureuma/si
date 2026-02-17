# PaaS Incident Event Schema

Date: 2026-02-17
Scope: WS12-01 incident schema, severity taxonomy, and dedupe strategy
Owner: Codex

## 1. Goal

Define a stable incident envelope so deploy hooks, health polls, and runtime collectors produce consistent records for agent execution and operator review.

## 2. Canonical Event Shape

Required fields:

1. `id`: unique incident event ID
2. `context`: active PaaS context (`internal-dogfood`, `oss-demo`, `customer-*`)
3. `source`: collector origin (for example `deploy-hook`, `health-poll`, `runtime-watch`)
4. `category`: one of `deploy|health|runtime|webhook|agent|unknown`
5. `severity`: one of `info|warning|critical`
6. `status`: one of `open|suppressed|resolved`
7. `message`: operator-facing summary
8. `dedupe_key`: stable key for equivalent incidents inside a dedupe window
9. `correlation_id`: stable ID to tie incident events to the same remediation run
10. `triggered_at`: event timestamp in UTC RFC3339Nano
11. `window_start`, `window_end`: dedupe window bounds

Optional fields:

1. `target`
2. `signal`
3. `metadata` (redacted via shared sensitive-field middleware)

## 3. Severity Taxonomy

`info`:

1. Non-actionable state transitions
2. Observability notices

`warning`:

1. Degraded state needing operator awareness
2. Retry or transient failure conditions

`critical`:

1. Hard failures requiring remediation or rollback
2. Security policy or isolation violations

Normalization rules:

1. `warn` maps to `warning`
2. `error` and `fatal` map to `critical`
3. Unknown values default to `info`

## 4. Dedupe Strategy

Default dedupe window:

1. 5 minutes (`paasIncidentDefaultDedupeWindow`)

Dedupe key input dimensions:

1. `context`
2. `source`
3. `category`
4. `severity`
5. `target`
6. `signal`
7. optional dedupe hint

The dedupe key is a short SHA256-derived stable digest over those normalized dimensions.

## 5. Correlation Strategy

`correlation_id` is derived from:

1. `dedupe_key`
2. dedupe `window_start`
3. optional external run reference (when available)

This keeps retries within the same window correlated while allowing new correlation groups for new windows.

## 6. Implementation Reference

Code primitives are implemented in:

1. `tools/si/paas_incident_schema.go`
2. `tools/si/paas_incident_schema_test.go`

Key functions:

1. `normalizePaasIncidentSeverity`
2. `normalizePaasIncidentCategory`
3. `resolvePaasIncidentDedupeWindow`
4. `buildPaasIncidentDedupeKey`
5. `buildPaasIncidentCorrelationID`
6. `newPaasIncidentEvent`

