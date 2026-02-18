# PaaS Agent Run Audit Artifacts

Date: 2026-02-18
Scope: WS12-10 per-run audit artifacts and incident correlation linkage
Owner: Codex

## 1. Goal

Persist a durable artifact for every agent run-log entry so each run can be audited independently with direct incident correlation.

## 2. Artifact Paths

Per-context artifact layout:

1. `contexts/<context>/events/agent-artifacts/<run-id>/<event-timestamp>.json`
2. run log index remains `contexts/<context>/events/agent-runs.jsonl`

`agent-runs.jsonl` now stores `artifact_path` for each row.

## 3. Artifact Payload

Each artifact stores:

1. `timestamp`
2. `context`
3. `source` (`agent-run`)
4. `record` (full `paasAgentRunRecord` snapshot)

`record` includes:

1. `incident_id`
2. `incident_correlation_id`
3. `policy_action`
4. `execution_mode`
5. `execution_note`
6. queue counters and runtime metadata

## 4. Command Output Linkage

The following commands now surface `artifact_path`:

1. `si paas agent run-once`
2. `si paas agent approve`
3. `si paas agent deny`
4. blocked `si paas agent run-once` lock path responses

## 5. Test Coverage

Coverage added in:

1. `tools/si/paas_agent_artifact_store_test.go`
2. `tools/si/paas_cmd_test.go` (`TestPaasAgentRunOnceOfflineFakeCodexDeterministicSmoke`)

Validation includes artifact file creation, decode, run-log linkage, and incident correlation propagation.
