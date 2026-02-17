# PaaS Incident Queue Storage and Retention

Date: 2026-02-17
Scope: WS12-03 context-scoped incident queue persistence
Owner: Codex

## 1. Queue Scope

Incident queue is context-scoped and stored at:

1. `contexts/<context>/events/incidents.jsonl`

Each entry keeps:

1. canonical incident payload
2. queue key (`dedupe_key|window_start`)
3. first/last seen timestamps
4. status
5. seen count

## 2. Retention Defaults

Defaults implemented in code:

1. `max_entries`: `1000`
2. `max_age`: `14d`
3. collection scan limit per sync: `200`

## 3. Retention Rules

1. Drop entries older than `max_age` by `last_seen`.
2. Sort newest-first by `last_seen`.
3. Truncate oldest entries when queue exceeds `max_entries`.

## 4. Upsert Rules

On collector sync:

1. queue key is `dedupe_key|window_start`
2. same key updates existing row (`seen_count++`, `last_seen` refresh)
3. severity can escalate (`info` -> `warning` -> `critical`)
4. metadata is merged with sensitive-field redaction

## 5. Implementation Reference

1. `tools/si/paas_incident_queue_store.go`
2. `tools/si/paas_incident_queue_store_test.go`

Primary entrypoint:

1. `syncPaasIncidentQueueFromCollectors(limit, maxEntries, maxAge)`

