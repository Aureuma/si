# PaaS Remediation Policy Engine

Date: 2026-02-17
Scope: WS12-06 remediation policy decisions
Owner: Codex

## 1. Policy Actions

Supported actions:

1. `auto-allow`
2. `approval-required`
3. `deny`

## 2. Context Storage

Policy file location:

1. `contexts/<context>/agents/remediation_policy.json`

## 3. Default Policy

Default action map:

1. `info -> auto-allow`
2. `warning -> approval-required`
3. `critical -> approval-required`

## 4. Engine Behavior

Policy evaluation:

1. normalize incident severity
2. apply severity override when defined
3. otherwise use default action
4. fallback to `approval-required` if config is invalid

## 5. Run-Once Integration

`si paas agent run-once` uses policy results when runtime adapter is ready:

1. `auto-allow` => run status `queued`
2. `approval-required` => run status `pending-approval`
3. `deny` => run status `denied`

Persisted run-log metadata:

1. `policy_action`
2. runtime adapter fields (`runtime_mode`, `runtime_profile`, `runtime_auth_path`, `runtime_ready`)

## 6. Implementation Reference

1. `tools/si/paas_agent_policy_engine.go`
2. `tools/si/paas_agent_policy_engine_test.go`
3. `tools/si/paas_agent_cmd.go` (`run-once` policy gating)

