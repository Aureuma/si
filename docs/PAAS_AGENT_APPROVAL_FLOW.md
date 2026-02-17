# PaaS Agent Approval Flow and Telegram Linkage

Date: 2026-02-17
Scope: WS12-07 approval flow
Owner: Codex

## 1. Commands

1. `si paas agent approve --run <id> [--note <text>]`
2. `si paas agent deny --run <id> [--note <text>]`

## 2. Persistence

Approval decisions are stored per context:

1. `contexts/<context>/agents/approvals.json`

Each decision stores:

1. `run_id`
2. `agent`
3. `decision` (`approved|denied`)
4. `note`
5. `actor`
6. `source`
7. `timestamp`

## 3. Run-Log Integration

Approve/deny operations append run-log records with status updates (`approved|denied`) so `si paas agent logs` reflects final operator decision status.

## 4. Telegram Callback Linkage

Approval decisions emit operational alerts with callback hints:

1. `callback_agent_approve`
2. `callback_agent_deny`

Alert emission path:

1. command handlers call `notifyPaasAgentApprovalTelegramLinkage`
2. alert records are written into `events/alerts.jsonl`
3. if Telegram is configured, callback-linked message is delivered

## 5. Implementation Reference

1. `tools/si/paas_agent_approval_store.go`
2. `tools/si/paas_agent_cmd.go`
3. `tools/si/paas_cmd_test.go` (`TestPaasAgentApproveDenyFlowPersistsDecision`)

