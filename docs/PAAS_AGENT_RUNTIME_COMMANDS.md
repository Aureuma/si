# PaaS Agent Runtime Commands

Date: 2026-02-17
Scope: WS12-04 live `si paas agent` command backend
Owner: Codex

## 1. Commands Implemented

Live (state-backed) commands:

1. `si paas agent enable`
2. `si paas agent disable`
3. `si paas agent status`
4. `si paas agent logs`
5. `si paas agent run-once`

## 2. Storage Model

Context-scoped state paths:

1. `contexts/<context>/agents/agents.json`
2. `contexts/<context>/events/agent-runs.jsonl`
3. incident queue source: `contexts/<context>/events/incidents.jsonl`

## 3. Behavioral Summary

`enable`:

1. Upserts agent config
2. Sets `enabled=true`
3. Persists targets/profile metadata

`disable`:

1. Requires existing agent
2. Sets `enabled=false`

`status`:

1. Lists one or all agents
2. Returns live metadata (enabled, targets, profile, last run state)

`logs`:

1. Reads run records from `agent-runs.jsonl`
2. Supports tail and optional follow flag contract

`run-once`:

1. Syncs incident queue from collector pipeline
2. Selects a queued incident (optional explicit `--incident`)
3. Records run result and queue stats
4. Updates agent `last_run_*` metadata

## 4. Implementation Reference

1. `tools/si/paas_agent_cmd.go`
2. `tools/si/paas_agent_store.go`
3. `tools/si/paas_cmd_test.go` (`TestPaasAgentEnableStatusRunOnceLogsDisable`)

