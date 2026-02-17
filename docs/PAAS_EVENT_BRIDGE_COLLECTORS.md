# PaaS Event Bridge Collectors

Date: 2026-02-17
Scope: WS12-02 deploy hook, health poll, and runtime event collectors
Owner: Codex

## 1. Goal

Map existing PaaS operational event streams into canonical incident events for agent workflows.

## 2. Collector Sources

Collectors consume context-scoped event logs:

1. `events/deployments.jsonl`
2. `events/alerts.jsonl`
3. `events/audit.jsonl`

## 3. Collector Types

`deploy-hook`:

1. Source: deploy events (`source=deploy`)
2. Trigger status: `failed|error|degraded|retrying`
3. Category: `deploy`

`health-poll`:

1. Source: alert events or health/ingress command patterns
2. Trigger status/severity: warning/critical or degraded/retrying/failed
3. Category: `health`

`runtime-watch`:

1. Source: audit events (`source=audit`)
2. Trigger status: `failed|error|degraded|retrying`
3. Category: `runtime`

## 4. Dedupe Behavior

Collectors emit incident candidates through WS12-01 schema primitives and dedupe by:

1. `dedupe_key`
2. `window_start`

Only the first candidate per dedupe bucket is kept.

## 5. Implementation Reference

Code:

1. `tools/si/paas_incident_collectors.go`
2. `tools/si/paas_incident_collectors_test.go`

Core entrypoint:

1. `collectPaasIncidentEvents(limit int)`

