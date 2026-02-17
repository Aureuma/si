# PaaS Agent Scheduler and Self-Heal Locking

Date: 2026-02-17
Scope: WS12-08 scheduler/self-heal controls
Owner: Codex

## 1. Locking Model

Each agent run uses a context-scoped lock:

1. `contexts/<context>/agents/locks/<agent>.lock.json`

Lock payload tracks:

1. `agent`
2. `owner`
3. `pid`
4. `acquired_at`
5. `heartbeat_at`

## 2. Scheduler Behavior

`agent run-once` now:

1. acquires lock before collector/queue processing
2. blocks run when lock is active
3. releases lock on completion

## 3. Self-Heal Behavior

Stale lock policy:

1. lock TTL is 15 minutes
2. if heartbeat/acquired timestamp is stale, lock is auto-recovered
3. recovered state is surfaced as `lock_recovered=true` in run output/logs

## 4. Blocked Run Behavior

When lock is active:

1. run status is `blocked`
2. lock reason is returned to caller
3. blocked run record is still appended for auditability

## 5. Implementation Reference

1. `tools/si/paas_agent_scheduler.go`
2. `tools/si/paas_agent_scheduler_test.go`
3. `tools/si/paas_cmd_test.go` (`TestPaasAgentRunOnceBlockedByActiveLock`)

