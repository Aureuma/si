# PaaS State Classification Policy

Last updated: 2026-02-17  
Scope: `si paas` local state, secret material, runtime data, and telemetry

## Policy Objective

Define mandatory data classes and allowed storage locations so operational state
for dogfood/customer contexts never leaks into OSS-tracked source locations.

## Data Classes and Storage Matrix

| Class | Examples | Allowed Storage | Forbidden Storage |
| --- | --- | --- | --- |
| `public_source` | source code, public docs, non-sensitive schemas | OSS repo tree | private vault/state roots |
| `private_state` | targets, release history, webhook mappings, addon config | `SI_PAAS_STATE_ROOT/contexts/<ctx>/...` (default `~/.si/paas/contexts/<ctx>/...`) | OSS repo tree |
| `private_secret` | SSH credentials, API tokens, webhook secrets, env secret values | `si vault` managed files and trust stores | OSS repo tree, plain command output, event logs |
| `runtime_data` | container volumes, DB files, queue persistence | target-node Docker volumes/host runtime paths | OSS repo tree, local control-plane state root |
| `audit_telemetry` | deploy/alert/audit events, operational status artifacts | context-scoped `events/` or private sinks | OSS repo tree unless explicitly redacted summaries |

## Context Boundary Requirements

1. Every stateful `si paas` operation resolves an active context (`default` or `--context`).
2. Reads/writes must stay under that single context path.
3. Cross-context transfer must be explicit (`si paas context export|import`) and non-secret by default.
4. Commands fail safe when context resolution or policy checks cannot be verified.

## Enforcement Mapping

1. Repo-state refusal guardrail blocks repo-local state roots unless explicit unsafe override is set.
2. Output redaction and plaintext guardrails prevent secret leakage in command output.
3. Context-scoped stores (`targets`, deploy history, addons, alert/audit/events) enforce per-context separation.
4. Export/import secret-like key rejection blocks accidental secret transport in metadata payloads.

## Review Checklist

1. New state file writes are context-scoped under `contexts/<ctx>/...`.
2. New command outputs avoid exposing secret-bearing fields.
3. New integration points define class + storage location in this policy.
4. Any exception path is documented with explicit unsafe-override semantics.
