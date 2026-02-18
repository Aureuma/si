# PaaS Agent Offline Fake-Codex Smoke Tests

Date: 2026-02-18
Scope: WS12-09 deterministic event-to-action smoke coverage
Owner: Codex

## 1. Goal

Validate the `si paas agent run-once` event-to-action loop without Codex auth/network by forcing an offline fake runtime.

## 2. Offline Runtime Switches

`agent run-once` supports an offline fake-codex action path when either:

1. `SI_PAAS_AGENT_OFFLINE_FAKE_CODEX=true`
2. `DYAD_CODEX_START_CMD` points at `fake-codex.sh`

Optional command override:

1. `SI_PAAS_AGENT_OFFLINE_FAKE_CODEX_CMD='<command>'`

If no command is provided, the runtime falls back to `tools/dyad/fake-codex.sh`.

## 3. Deterministic Smoke Contract

When an incident is selected and policy action is `auto-allow`, `agent run-once` now emits:

1. `execution_mode` (`offline-fake-codex` or `deferred`)
2. `execution_note` (compacted deterministic report summary)
3. `incident_correlation_id`

These fields are persisted in:

1. `contexts/<context>/events/agent-runs.jsonl`
2. command output envelope (`si paas agent run-once --json`)

## 4. Test Coverage

Deterministic smoke coverage is implemented in:

1. `tools/si/paas_cmd_test.go` (`TestPaasAgentRunOnceOfflineFakeCodexDeterministicSmoke`)
2. `tools/si/paas_agent_offline_fake_codex_test.go`

The smoke test validates full loop behavior:

1. deploy failure event collection
2. incident queue selection
3. policy auto-allow decision
4. offline fake-codex execution
5. execution metadata persistence in run logs
