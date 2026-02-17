# PaaS Agent Runtime Adapter (Codex Profile Auth Path)

Date: 2026-02-17
Scope: WS12-05 dyad-style runtime adapter
Owner: Codex

## 1. Goal

Bridge `si paas agent run-once` with Codex profile identity and auth-cache state before remediation execution.

## 2. Adapter Mode

Implemented adapter mode:

1. `codex-profile-auth`

Adapter validates:

1. agent profile selection (`agent.profile` or default first configured profile)
2. profile existence in Codex profile registry
3. auth cache readiness via profile auth path status

## 3. Output Contract

Adapter plan fields:

1. `mode`
2. `profile_id`
3. `profile_email`
4. `auth_path`
5. `incident_id`
6. `prompt`
7. `ready`
8. `reason` (when blocked)

## 4. Run-Once Integration

`si paas agent run-once` now:

1. builds adapter plan per selected incident
2. marks run state `blocked` when profile/auth prerequisites are not ready
3. persists runtime metadata into run logs:
   - `runtime_mode`
   - `runtime_profile`
   - `runtime_auth_path`
   - `runtime_ready`

## 5. Implementation Reference

1. `tools/si/paas_agent_runtime_adapter.go`
2. `tools/si/paas_agent_runtime_adapter_test.go`
3. `tools/si/paas_agent_cmd.go` (`run-once` integration)

