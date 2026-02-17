# Ticket: PaaS State Isolation Model (Dogfood vs OSS vs Customer Contexts)

Date: 2026-02-17
Owner: Unassigned
Status: Planned
Priority: Critical

## 1. Objective

Define and implement a strict isolation model so `si paas` can be used for internal dogfooding and customer operations without leaking private operational state into the open-source repository.

## 2. Non-Negotiable Rules

1. Public repository contains source code and public docs only.
2. Private operational state must live outside the repo by default.
3. Secrets never appear in git-tracked files.
4. Context boundaries are mandatory for all stateful operations.
5. Commands must fail safe when boundary checks cannot be verified.

## 3. Data Classification

| Class | Description | Allowed Location | Forbidden Location |
| --- | --- | --- | --- |
| `public_source` | code, docs, schemas safe for OSS | OSS repo | private vault/state roots |
| `private_state` | targets, releases, deployment history, mappings | `~/.si/paas/contexts/<ctx>/state.db` | OSS repo |
| `private_secret` | SSH creds, API keys, webhook secrets, env values | `si vault` private storage | OSS repo and plaintext logs |
| `runtime_data` | service volumes, app data, DB files | target node Docker volumes/paths | OSS repo |
| `audit_telemetry` | operation events, alerts, audit logs | context event roots/private sinks | OSS repo unless redacted summaries |

## 4. Filesystem Layout

Default root:
- `~/.si/paas/contexts/`

Per-context layout:
- `~/.si/paas/contexts/<context>/state.db`
- `~/.si/paas/contexts/<context>/events/`
- `~/.si/paas/contexts/<context>/cache/`
- `~/.si/paas/contexts/<context>/config.json`

Recommended context names:
1. `internal-dogfood`
2. `oss-demo`
3. `customer-<id>`

## 5. Context Model

Each context must define:

1. `name`
2. `type` (`internal`, `demo`, `customer`)
3. `state_root`
4. `vault_file`
5. `default_targets`
6. `webhook_policy`
7. `audit_sink`

Boundary behavior:

1. Commands read and write only the active context.
2. Cross-context copy requires explicit `export/import`.
3. `export` excludes secrets by default.
4. MVP requires one vault file per context (no shared vault file across contexts).

## 6. Command Contract

Required commands:

1. `si paas context create <name> [--type ...] [--state-root ...] [--vault-file ...]`
2. `si paas context list`
3. `si paas context use <name>`
4. `si paas context show [<name>]`
5. `si paas context remove <name>`
6. `si paas doctor`

Global flag:
- `--context <name>` for all stateful `si paas` operations.

## 7. Safety Guardrails

1. Refuse state root under a git workspace by default.
2. Refuse commands when context has missing vault mapping for secret-requiring operations.
3. Redact secrets from logs/errors/events.
4. Refuse insecure export that includes secrets unless explicit dangerous override is supplied.
5. Validate webhook policies per context before enabling trigger endpoints.

## 8. Dogfooding Operating Model

Internal usage should run under `internal-dogfood` context:

1. Separate state root.
2. Separate vault file.
3. Separate target inventory.
4. Separate webhook secret namespace.
5. Separate audit sink and retention policy.

`oss-demo` context should only point to disposable/non-sensitive resources.

## 9. Backup and Recovery

Back up the following per context:

1. `state.db`
2. `events/`
3. context config

Never back up plaintext secrets from runtime outputs.
Vault backup follows vault policy, not PaaS state backup tooling.

## 10. Acceptance Criteria

1. Running `si paas context create internal-dogfood` creates isolated state directories outside repo.
2. `si paas doctor` fails when state path resolves under repo.
3. Deploys in one context do not appear in `si paas ...` queries for another context.
4. Exports from one context omit secrets by default.
5. Attempting to run without a resolvable context returns actionable error.
6. CI tests enforce “no private state files under repo tree”.

## 11. Implementation Tasks

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| ISO-01 | Add context config schema and persistence | Not Started | Unassigned | Linked WS: WS11-01, WS11-02 |
| ISO-02 | Add global `--context` resolution pipeline | Not Started | Unassigned | Linked WS: WS02-06 |
| ISO-03 | Add context CRUD commands | Not Started | Unassigned | Linked WS: WS11-02 |
| ISO-04 | Add state-root safety checks (`git` workspace refusal) | Not Started | Unassigned | Linked WS: WS11-03 |
| ISO-05 | Add secret redaction middleware for events and logs | Not Started | Unassigned | Linked WS: WS05-03, WS11-03 |
| ISO-06 | Add context isolation integration tests | Not Started | Unassigned | Linked WS: WS09-05 |
| ISO-07 | Add `si paas doctor` policy checks | Not Started | Unassigned | Linked WS: WS11-03 |
| ISO-08 | Document dogfood runbook and backup policy | Not Started | Unassigned | Linked WS: WS11-04, WS11-05 |

## 12. Decisions and Deferred Items

1. Decision (MVP): enforce one vault file per context; shared vault files across contexts are out of scope in MVP.
2. Deferred: per-context RBAC metadata is deferred to post-research cloud scope (Phase D+), not required for MVP CLI delivery.
3. Decision (MVP): `si paas doctor` critical isolation failures are blocking by default for deploy and secret-mutating commands.
