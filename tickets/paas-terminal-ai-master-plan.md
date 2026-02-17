# Ticket: AI-First Terminal PaaS (Docker Compose First) - Master Plan

Date: 2026-02-17
Owner: Unassigned
Status: In Progress (Phase B kickoff)
Scope: Design and implementation plan for a self-hosted + cloud-hosted PaaS, with MVP delivered as `si` CLI only (no web dashboard and no full-screen TUI in MVP).

## 1. Requirements Snapshot

This plan is constrained by the following hard requirements:

1. MVP must be terminal-first inside `si` (no web dashboard in MVP).
2. Docker-first is mandatory.
3. Docker Compose is mandatory for MVP.
4. Docker Swarm, k3s, and Kubernetes are explicitly out of scope for MVP.
5. Self-hosted edition is free.
6. Cloud-hosted edition is managed and paid.
7. AI must be core to operations.
8. Codex CLI integration is required in MVP.
9. Telegram is first-choice notification channel (email not required in MVP).
10. Multi-VPS control over SSH is required.
11. Secrets and credentials must integrate with `si vault`.
12. Plan must support multiple parallel agent coders and ongoing progress updates.
13. MVP delivery excludes full-screen TUI work (CLI only).
14. Dogfooding state must be isolated from open-source repository contents by design.
15. Long-running infra agents must run on Codex CLI subscription path in MVP (no mandatory direct LLM API integration).

## 2. Existing Assets We Should Reuse

The current repos already provide leverage:

1. `si`:
- Existing command architecture for new root command groups.
- Existing Docker control patterns (`si spawn`, `si dyad`, `si docker`).
- Existing dyad actor/critic closed-loop runtime with control files and report artifacts.
- Existing interactive CLI selection patterns for subcommands.
- Existing vault lifecycle (`si vault init/set/get/run/docker exec/trust/recipients`).
- Existing provider integrations and guardrail patterns.

2. `viva`:
- Proven Docker Compose operational patterns across dev/prod.
- Existing secret-source fallback patterns (`si vault` or external).
- Existing infra operational script style and runbook rigor.

3. `openclaw` (f.k.a. clawdbot/moltbot lineage):
- Gateway-centric control-plane design patterns.
- Multi-agent coordination patterns (`sessions_*` tools, role-driven orchestration).
- Remote gateway and operational runbook patterns.

## 3. Design Decisions (ADR Summary)

### ADR-001: MVP Interface = `si` CLI only

Decision:
- Add `si paas` command family as the canonical MVP CLI interface.

Why:
- Matches explicit requirement to avoid web dashboard in MVP.
- Fits existing `si` UX and command-dispatch model.
- Maximizes compatibility with AI coders and non-interactive automation.

Alternatives considered:
1. Build SvelteKit dashboard first.
Reason not chosen: violates MVP constraint and increases delivery risk.
2. Build full-screen TUI first.
Reason not chosen: slows MVP and weakens automation reliability.
3. Build standalone new CLI binary.
Reason not chosen: duplicates auth/settings/vault/Docker logic already in `si`.

### ADR-002: Deployment Runtime = Docker Compose on target nodes

Decision:
- Use Compose bundles as the deployable unit (`compose.yaml` + release metadata).

Why:
- Matches hard requirement.
- Broad operator familiarity and low operational burden.

Alternatives considered:
1. Docker Swarm.
Reason not chosen: explicitly disallowed in MVP.
2. k3s.
Reason not chosen: explicitly disallowed in MVP.
3. Kubernetes.
Reason not chosen: explicitly disallowed in MVP and too heavy for MVP.

### ADR-003: Primary implementation language = Go

Decision:
- Build core PaaS control logic in Go within `tools/si`.

Why:
- `si` is already Go-based.
- Faster integration, lower cognitive overhead, lower delivery risk.
- Strong Docker/SSH/process tooling ecosystem.

Alternatives considered:
1. Rust core.
Reason not chosen: excellent performance but slower team iteration for this codebase context.
2. TypeScript core.
Reason not chosen: weaker fit for current `si` architecture and binary distribution model.

### ADR-004: Node control path = SSH transport first, optional gateway later

Decision:
- MVP uses direct SSH execution to remote VPS nodes.
- Defer persistent remote gateway/agent daemon to Phase 2+.

Why:
- Fastest path for multi-VPS management with existing credentials.
- Minimal moving parts for MVP.

Alternatives considered:
1. Mandatory gateway daemon from day one.
Reason not chosen: adds operational overhead and failure surface before core deploy pipeline is proven.
2. Docker TCP daemon exposure.
Reason not chosen: higher security risk and harder hardening baseline.

### ADR-005: Secrets = `si vault` as system-of-record for operator secrets

Decision:
- Store target credentials, app env, and provider tokens in `si vault` encrypted dotenv files.

Why:
- Already integrated and documented in this repo.
- Supports local and container execution with controlled injection.

Alternatives considered:
1. New dedicated secrets subsystem.
Reason not chosen: reinvents solved capabilities and delays delivery.
2. Plain `.env` files.
Reason not chosen: unacceptable security posture.

### ADR-006: Notifications = Telegram-first

Decision:
- MVP notification adapter supports Telegram Bot API first.

Why:
- Explicit preference.
- Low setup friction and good terminal-friendly ops pattern.

Alternatives considered:
1. Email-first.
Reason not chosen: explicitly de-prioritized.
2. Slack-first.
Reason not chosen: useful later, not MVP priority.

### ADR-007: AI orchestration = Codex adapter first, pluggable provider interface

Decision:
- Codex CLI adapter is first-class in MVP.
- Provider interface allows Claude/Gemini adapters later.

Why:
- Explicit requirement that Codex is integrated at the heart of operations.
- Maintains future flexibility.

Alternatives considered:
1. One hardcoded model/provider forever.
Reason not chosen: long-term lock-in and reduced resilience.

### ADR-008: MVP stays non-TUI; optional TUI is post-MVP only

Decision:
- Keep line-oriented CLI commands as the primary interface for all `si paas` operations.
- Do not implement full-screen TUI during MVP phases.
- Revisit optional TUI only after MVP stability gates are complete.

Why:
- AI coders (Codex/Claude-style) operate best with deterministic, non-interactive stdout/stderr.
- Existing `si` command style already supports automation and remote execution workflows.

Alternatives considered:
1. Migrate all `si` commands to Bubble Tea full-screen interfaces.
Reason not chosen: degrades non-interactive automation and parser stability.
2. Keep everything prompt-based without any richer UX forever.
Reason not chosen: post-MVP operator ergonomics may benefit from optional TUI.

### ADR-009: Dogfood and customer operational state must be context-isolated from OSS code

Decision:
- Treat repository code and operational state as separate domains.
- Introduce context-scoped state roots for internal dogfood, OSS demo, and customer operations.
- Keep mutable runtime state, secrets, and audit data out of the public source repository.

Why:
- Prevent accidental leakage of private targets, credentials, and operational metadata.
- Allow teams to dogfood in production-like conditions without contaminating OSS history.
- Enable clean multi-tenant and multi-environment operations from one CLI.

Alternatives considered:
1. Single global state file for all environments.
Reason not chosen: high blast radius, hard segregation, easy leakage.
2. State files inside repo workspace.
Reason not chosen: unsafe defaults and high commit-leak risk.

### ADR-010: Monetized cloud offering uses simple subscription tiers for solo-dev ICP

Decision:
- Use flat monthly subscription tiers for cloud-hosted customers.
- Keep one primary billable metric: active app slots.
- Avoid fine-grained usage-based billing (CPU, bandwidth, requests, token-based) in initial monetized release.

Why:
- Solo-dev and solopreneur buyers prefer predictable bills.
- Simpler billing model is easier to understand, implement, and support.
- Entitlement checks at deploy time are operationally straightforward.

Alternatives considered:
1. Full usage-based metering from day one.
Reason not chosen: high implementation complexity and poor billing predictability for ICP.
2. Seat-based pricing as primary metric.
Reason not chosen: most ICP accounts are single-owner and not seat-heavy.
3. Hybrid base + overage pricing.
Reason not chosen: adds billing ambiguity and support overhead too early.

### ADR-011: Stateful infra agents use existing dyad-style Codex runtime and event bridge

Decision:
- Implement long-running operations agents as `si paas agent` workers backed by actor/critic loop patterns already proven in `si dyad`.
- Feed agents through a normalized incident/event queue and policy engine.
- Use Codex CLI profile auth/subscription path as primary inference runtime in MVP.

Why:
- Reuses existing interactive Codex loop control model and lowers implementation risk.
- Avoids mandatory incremental LLM API costs for continuous operations automation.
- Keeps agent behavior inspectable through existing report artifacts and control files.

Alternatives considered:
1. Direct LLM API event processors only.
Reason not chosen: higher recurring costs and a second runtime path to maintain.
2. Build new custom multi-agent runtime from scratch.
Reason not chosen: slower delivery and duplicates working dyad mechanics.

## 4. Target Architecture (MVP)

### 4.1 High-level

1. Operator runs `si paas ...` from terminal.
2. `si` resolves project state + secrets via settings and vault.
3. `si` packages deployment bundle.
4. `si` connects via SSH to target VPS and executes deployment actions.
5. `si` stores release metadata, deployment history, and health snapshots.
6. `si` sends Telegram notifications for deploy/rollback/incidents.
7. Optional AI flows (`si paas ai ...`) invoke Codex-assisted planning/remediation.

### 4.2 Core modules to add under `tools/si`

1. `paas_cmd.go`: root command router (`si paas`).
2. `paas_target_cmd.go`: target/node lifecycle (`add/list/use/check/remove`).
3. `paas_app_cmd.go`: app lifecycle (`init/config/list/status/remove`).
4. `paas_deploy_cmd.go`: release creation, deploy, rollback.
5. `paas_logs_cmd.go`: logs tail/search per target/service.
6. `paas_alert_cmd.go`: Telegram notifier config/test/history.
7. `paas_ai_cmd.go`: Codex-powered plan/check/fix flows.
8. `paas_store.go`: persistent metadata storage abstraction.
9. `paas_ssh.go`: SSH command/file transport abstraction.
10. `paas_compose.go`: Compose orchestration primitives.
11. `paas_agent_cmd.go`: long-running agent lifecycle (`enable/disable/status/run-once`).
12. `paas_event_bridge.go`: event collection, normalization, and queue writing.
13. `paas_agent_policy.go`: remediation policy and approval guardrails.

### 4.3 Suggested state model (MVP)

Local metadata store path (context-scoped):
- `~/.si/paas/contexts/<context>/state.db` (SQLite)

Entities:
1. Target
- id, context_id, host, port, user, auth_method, labels, default.
2. App
- id, context_id, slug, repo, compose_template_path, default_target_group.
3. Release
- id, context_id, app_id, version, image digests, created_at, created_by.
4. Deployment
- id, context_id, release_id, target_id, status, started_at, finished_at, logs_ref.
5. AlertEvent
- id, context_id, severity, scope, message, channel, sent_at, ack_state.
6. IncidentEvent
- id, context_id, source, target_id, app_id, type, severity, payload_json, dedupe_key, detected_at, status.
7. AgentRun
- id, context_id, agent_id, incident_id, action_plan_ref, decision, started_at, finished_at, result, report_ref.

### 4.4 Remote execution strategy

1. Bootstrap target host with preflight checks:
- Docker installed and reachable.
- Compose plugin available (`docker compose version`).
- Required disk/network baseline.

2. Deploy flow:
- Upload release bundle to target (scp/rsync).
- Run `docker compose pull`.
- Run `docker compose up -d --remove-orphans`.
- Run health checks.
- Mark deployment success/failure.

3. Rollback flow:
- Resolve previous known-good release.
- Re-run compose with pinned release digest/env.

### 4.5 Credential handling strategy

1. Target credentials and secrets are vaulted.
2. Password-based SSH support is allowed for bootstrap only.
3. Key-based auth migration is a required post-bootstrap task.
4. Plaintext secrets are never persisted in git-tracked files.

### 4.6 Reconciliation and drift control strategy

1. Periodically reconcile local `si paas` state against actual Docker/Compose runtime state on each target.
2. Mark divergences explicitly (`missing`, `unmanaged`, `drifted`, `orphaned`).
3. Provide non-destructive auto-repair proposals before mutating runtime state.
4. Keep reconciliation idempotent and safe under repeated execution.

### 4.7 Ingress strategy (per-node, Compose-only MVP)

1. Each target node runs a local ingress service managed by Compose.
2. MVP ingress provider is fixed to Traefik.
3. Caddy remains a documented post-MVP alternative if Traefik blockers emerge.
4. Define DNS/LB model separately for:
- single-node apps
- active-passive multi-node apps
- round-robin multi-node apps
5. Keep ingress policy Swarm/K8s-independent.

### 4.8 Magic variables and add-on packs

1. Define reserved runtime variables resolved by `si paas` before deploy (for example app, target, release, and environment metadata).
2. Keep magic-variable resolution deterministic and validated before `docker compose` is executed.
3. Define add-on packs (DB/cache/queue/storage) as reusable Compose fragments with explicit merge rules.
4. Support per-app overrides without breaking base add-on pack defaults.

### 4.9 State isolation model (dogfood vs OSS)

Data classes:
1. Public source artifacts.
2. Private control-plane state (targets, releases, deployments, webhook mappings).
3. Private secrets (SSH creds, API keys, webhook secrets, env values).
4. Private runtime data (volumes, service data, backups).
5. Private operational telemetry and audit events.

Allowed locations:
1. Public source artifacts:
- inside OSS repo (`/home/shawn/Development/si`).
2. Private control-plane state:
- `~/.si/paas/contexts/<context>/state.db`
- `~/.si/paas/contexts/<context>/events/`
3. Private secrets:
- `si vault` private files/repo only.
4. Private runtime data:
- Docker volumes/host paths on target nodes only.
5. Private telemetry/audit:
- context-specific local paths or private sinks.

Guardrails:
1. Refuse to initialize PaaS state inside a git workspace by default.
2. Refuse to print secret material in normal command output.
3. Enforce context-scoped vault file and target registry resolution.
4. Require explicit override flags for risky export/debug actions.
5. Add `si paas doctor` checks for repo contamination risk.

### 4.10 Context model for isolation

Context types:
1. `internal-dogfood`
2. `oss-demo`
3. `customer-<id>`

Each context owns:
1. State root path.
2. Vault file/default secret scope.
3. Target inventory and SSH policies.
4. Webhook secrets and trigger rules.
5. Audit/event sink config.

Context boundary rule:
- No cross-context reads/writes unless explicit import/export command is used.

### 4.11 Monetization model (Solo-dev / Solopreneur ICP)

Packaging model:
1. Self-hosted OSS edition:
- free software, user-managed infrastructure.
2. Cloud-hosted managed edition:
- paid subscription plans with clear limits.

Primary billable metric:
1. Active app slots.
- One slot equals one managed app with at least one active deployment.
- Multi-container compose app still counts as one app slot.

Secondary entitlement limits (non-primary metrics):
1. Managed target count.
2. Log retention days.
3. Concurrent deploy operations.

Billing model:
1. Flat monthly plans (no per-request/per-GB/per-token overage in v1).
2. Optional annual discount can be added later after baseline stability.
3. Upgrades apply immediately.
4. Downgrades apply at next billing cycle boundary.

Failure and grace behavior:
1. On payment failure, enter `grace` for 7 days.
2. During `grace`, allow read operations, billing recovery actions, and rollback to last known-good release.
3. During `grace`, block mutating growth actions (`deploy`, `scale`, `app init/remove`, `target add/remove`, `secret set/unset`).
4. After grace timeout, enter `restricted` and block all mutating operations until billing is restored.
5. Return to `active` immediately on successful payment recovery webhook.

User-facing clarity principles:
1. Always show current plan and remaining limits in CLI (`si paas cloud usage`).
2. On limit hit, return actionable upgrade guidance.
3. No hidden usage charges in initial release.

### 4.12 Stateful agent runtime (event-driven, Codex subscription path)

Runtime topology:
1. Event bridge ingests signals from deploy hooks, periodic health polls, and Docker runtime events on managed targets.
2. Signals are normalized into `IncidentEvent` records with severity, scope, and dedupe keys.
3. Dispatcher maps incidents to agent work items and queues them per context.
4. Agent workers run as long-lived processes using dyad-style actor/critic interaction patterns (persistent loop + control files + report artifacts).
5. Policy layer decides whether actions are:
- auto-allowed (safe low-risk remediations)
- approval-required (scale/destructive/network/security-impacting operations)
- denied (violates policy guardrails)
6. All actions write auditable event and artifact records before and after execution.

Control model:
1. `si paas agent enable --name <agent>` starts or reconciles an agent worker.
2. `si paas agent disable --name <agent>` requests clean stop and persists disabled state.
3. `si paas agent run-once --name <agent>` executes one deterministic cycle for testing.
4. `si paas agent status --name <agent> --json` returns machine-readable live state.
5. `si paas agent approve|deny --run <id>` resolves policy-gated actions.

Policy baseline for MVP:
1. Auto-allowed:
- restart failed app service
- re-run last known-good deploy
- trigger non-destructive reconcile
2. Approval-required:
- scale replicas/resources
- rollback across multiple targets
- secret/material config mutation
3. Denied by default:
- destructive data operations
- firewall/network policy mutation outside approved scope

## 5. CLI Specification (MVP, Non-TUI)

Primary command family:
- `si paas`

Proposed subcommands:

1. `si paas target add`
2. `si paas target list`
3. `si paas target check`
4. `si paas target use`
5. `si paas target remove`
6. `si paas app init`
7. `si paas app list`
8. `si paas app status`
9. `si paas app remove`
10. `si paas deploy`
11. `si paas rollback`
12. `si paas logs`
13. `si paas alert setup-telegram`
14. `si paas alert test`
15. `si paas alert history`
16. `si paas ai plan`
17. `si paas ai inspect`
18. `si paas ai fix`
19. `si paas context create`
20. `si paas context list`
21. `si paas context use`
22. `si paas context show`
23. `si paas context remove`
24. `si paas agent enable`
25. `si paas agent disable`
26. `si paas agent status`
27. `si paas agent logs`
28. `si paas agent run-once`
29. `si paas agent approve`
30. `si paas agent deny`
31. `si paas events list`

Deploy fan-out UX (multi-VPS):
1. `si paas deploy --target <id>` (default single target)
2. `si paas deploy --targets <id1,id2,...>`
3. `si paas deploy --targets all`
4. `si paas deploy --targets all --strategy serial|rolling|canary|parallel`
5. `si paas deploy --targets all --max-parallel <n>`
6. `si paas deploy --targets all --continue-on-error`

Interactive behavior:
- Running `si paas` with no subcommand should open a numbered command picker like existing dyad/vault flows.
- Interactive prompts are convenience only; they must never be required.

CLI/TUI compatibility policy:
- All critical `si paas` commands must support non-interactive execution.
- Add stable machine output mode (`--json`) to operational commands.
- Avoid mandatory full-screen rendering in default execution paths.
- Do not ship full-screen TUI in MVP milestones.
- Add global `--context <name>` support for all stateful PaaS operations.

Agent command contracts (MVP):
1. `si paas agent enable --name <agent> --targets <id|all> --profile <codex_profile>`
- Starts or reconciles long-running agent worker for the context.
2. `si paas agent disable --name <agent>`
- Stops worker cleanly and persists disabled state.
3. `si paas agent status [--name <agent>] [--json]`
- Reports worker state, queue depth, last run result, and last incident handled.
4. `si paas agent run-once --name <agent> [--incident <id>]`
- Executes one deterministic loop iteration for testing and CI.
5. `si paas agent approve --run <id>` and `si paas agent deny --run <id>`
- Resolves approval-gated remediation actions.
6. `si paas events list [--severity ...] [--status ...] [--json]`
- Lists normalized incident stream for operators and automation.

## 6. Competitive Research Program (Completed 2026-02-17; Secondary Baselines Ongoing)

This section defined the deep-discovery protocol used for Phase A.
Primary-set research outputs are complete; secondary-set refinement continues as non-blocking background work.

### 6.1 Platforms to analyze deeply

Primary set:
1. Coolify
- Repo: `https://github.com/coollabsio/coolify`
- Docs: `https://coolify.io/docs/`
2. CapRover
- Repo: `https://github.com/caprover/caprover`
- Docs: `https://caprover.com/docs/get-started.html`
3. Dokploy
- Repo: `https://github.com/Dokploy/dokploy`
- Docs: `https://docs.dokploy.com/docs/core`
4. Dokku
- Repo: `https://github.com/dokku/dokku`
- Docs: `https://dokku.com/docs/getting-started/installation/`

Secondary set:
5. Easypanel
- Site/docs entry: `https://easypanel.io/`
6. Portainer (as adjacent control-plane baseline)
- Repo: `https://github.com/portainer/portainer`
7. Tsuru (non-MVP orchestration baseline)
- Repo: `https://github.com/tsuru/tsuru`
8. OpenClaw architecture patterns
- Repo: `https://github.com/openclaw/openclaw`
9. SwiftWave (lightweight Go-focused reference)
- Repo: `https://github.com/swiftwave-org/swiftwave`
- Docs/site: `https://swiftwave.org/`
10. Kamal (deploy workflow reference)
- Repo: `https://github.com/basecamp/kamal`
- Docs: `https://kamal-deploy.org/`

### 6.2 Deep web feedback collection protocol

For each platform above, collect and label user sentiment from:

1. GitHub issues (open + closed, high-comment threads).
2. GitHub discussions.
3. Reddit threads.
4. Hacker News discussions.
5. Discord/Forum posts where publicly indexable.
6. Independent blog migration reports.

Required evidence fields per item:
1. Source URL
2. Date (absolute date)
3. User type if inferable (solo dev, startup, agency, enterprise)
4. Positive signal summary
5. Negative signal summary
6. Feature request or missing capability
7. Confidence score (`high`, `medium`, `low`)

### 6.3 Codebase analysis protocol per platform

For each cloned repo:

1. Architecture map:
- Runtime components
- Data stores
- Deployment model
- Agent/background jobs

2. Operational capabilities:
- Deploy model
- Rollback model
- Multi-node model
- Secret model
- Logging/metrics/alerting model

3. Extensibility:
- Plugin model
- API surface
- Automation hooks

4. Failure mode review:
- Common incident classes
- Recovery ergonomics
- Upgrade/migration risks

5. Security posture:
- AuthN/AuthZ model
- Secret storage assumptions
- Remote execution surface

### 6.4 Required research outputs

1. `tickets/paas-competitive-research-board.md` updated with evidence links.
2. One research summary section per platform:
- Strengths users love
- Weaknesses users report
- Missing features users request
- Lessons we should copy
- Mistakes we should avoid
3. Final comparative matrix and prioritized feature shortlist.

### 6.5 Research-driven plan updates (applied)

1. Reliability priority: deterministic deploy failure taxonomy with remediation hints is now an explicit Phase B deliverable.
2. Compatibility priority: architecture/runtime preflight checks are now explicit to reduce deploy mismatch and upgrade regressions.
3. Ingress resilience priority: TLS/ACME retry observability and recovery signaling are now explicit deliverables.
4. Lifecycle priority: deployment retention/pruning controls are now explicit in deploy engine scope.
5. Scope control: Easypanel/Portainer/Tsuru remain secondary baselines and are not Phase B blockers.

## 7. Workstreams (Parallel Execution Plan)

Each workstream is independently executable by a different agent.

Status values:
- `Not Started`
- `In Progress`
- `Blocked`
- `Done`

### WS-00 Program Setup

Goal:
- Set up structure, conventions, and acceptance gates for all other streams.

Dependencies:
- None

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS00-01 | Create `si paas` ticket/docs index | Not Started | Unassigned | |
| WS00-02 | Define coding/test standards for new `paas_*` files | Not Started | Unassigned | |
| WS00-03 | Define release branch and milestone cadence | Not Started | Unassigned | |

### WS-01 Competitive Research and State-of-the-Art Synthesis

Goal:
- Produce evidence-backed market and architecture analysis.

Dependencies:
- WS-00

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS01-01 | Clone and index all primary competitor repos | Done | Codex | Cloned under `.tmp/paas-research` |
| WS01-02 | Collect user feedback evidence corpus | Done | Codex | Evidence log updated in `tickets/paas-competitive-research-board.md` |
| WS01-03 | Analyze strengths/weaknesses per platform | Done | Codex | Findings sections completed for primary set + SwiftWave/Kamal |
| WS01-04 | Publish consolidated findings + feature priority list | Done | Codex | Comparative matrix + approved shortlist added |
| WS01-05 | Expand analysis set with SwiftWave and Kamal references | Done | Codex | Canonical upstreams + evidence captured |

### WS-02 CLI Domain Model and UX

Goal:
- Define and implement `si paas` command surface for automation-safe CLI UX.

Dependencies:
- WS-00

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS02-01 | Add `paas` root command wiring in `root_commands.go` | Done | Codex | Root dispatch + top-level usage wired (`root_commands.go`, `util.go`) |
| WS02-02 | Implement complete non-interactive command and flag surfaces for all MVP operations | Done | Codex | Added full subcommand scaffolding for target/app/deploy/rollback/logs/alert/ai/context/agent/events |
| WS02-03 | Add stable machine-readable output modes (`--json`) for operational commands | Done | Codex | Added shared scaffold JSON envelope and `--json` flag handling across MVP `si paas` command surfaces |
| WS02-04 | Add command tests for dispatch, non-interactive behavior, and output contracts | Done | Codex | Added `paas_cmd_test.go` coverage for usage behavior, JSON contract envelopes, context propagation, and action-set/dispatch parity |
| WS02-05 | Add optional prompt helpers only where they do not block non-interactive execution | Done | Codex | Added optional interactive command pickers for `si paas` and subcommand groups while preserving non-interactive usage behavior |
| WS02-06 | Add context command surface and global `--context` routing | Done | Codex | Global `--context` parsing added at `si paas` root with context propagated in text/JSON output envelopes |

### WS-03 Multi-VPS Target Management (SSH)

Goal:
- Manage and verify multiple VPS targets through secure SSH workflows.

Dependencies:
- WS-02

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS03-01 | Add target model + local storage CRUD | Done | Codex | Added context-scoped target store (`targets.json`) and live CRUD wiring for `target add/list/use/remove` |
| WS03-02 | Implement SSH connectivity + preflight checks | Done | Codex | Added live target preflight checks (TCP reachability, SSH, Docker, Compose) with structured diagnostics and non-zero exit on failures |
| WS03-03 | Implement bootstrap path from password to key auth | Done | Codex | Added `si paas target bootstrap` with password-env + public-key flow and auth-method promotion to `key` on success |
| WS03-04 | Add `si paas target check --all` health summary | Done | Codex | `target check --all` now executes live per-target preflights with aggregate text/JSON summary and failure exit codes |
| WS03-05 | Implement Traefik per-node ingress baseline with DNS/LB model; keep Caddy as post-MVP alternative | Done | Codex | Added `target ingress-baseline` with Traefik artifact rendering and persisted DNS/LB ingress metadata per target |
| WS03-06 | Implement architecture/runtime compatibility preflight (`cpu arch`, Docker/Compose version, image platform) with actionable failures | Done | Codex | Added architecture/runtime preflight checks including `--image-platform` compatibility validation and actionable mismatch diagnostics |

### WS-04 Deployment Engine (Compose-first)

Goal:
- Deploy and rollback Docker Compose apps reliably.

Dependencies:
- WS-02, WS-03

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS04-01 | Define release bundle format and metadata | Done | Codex | `si paas deploy` now materializes context-scoped release bundle directories with bundled `compose.yaml` and `release.json` metadata (release id, digest, targets, strategy, guardrail summary) |
| WS04-02 | Implement remote upload and compose apply | Not Started | Unassigned | |
| WS04-03 | Implement health checks and rollback orchestration | Not Started | Unassigned | |
| WS04-04 | Implement deployment logs and event recording | Not Started | Unassigned | |
| WS04-05 | Implement runtime reconciler and drift repair planning | Not Started | Unassigned | |
| WS04-06 | Define and implement Compose-only blue/green cutover and rollback policy per node | Not Started | Unassigned | |
| WS04-07 | Define service-pack/add-on contract (DB/cache/queue) and lifecycle operations | Not Started | Unassigned | |
| WS04-08 | Implement parallel deploy fan-out engine and strategy flags (`serial`, `rolling`, `canary`, `parallel`) | Not Started | Unassigned | |
| WS04-09 | Implement Git webhook ingestion with auth validation and app/branch trigger mapping | Not Started | Unassigned | |
| WS04-10 | Implement magic-variable resolution and add-on compose-fragment merge validation | Not Started | Unassigned | |
| WS04-11 | Implement deterministic deploy failure taxonomy + remediation hint output contract | Not Started | Unassigned | Research-driven P0 diagnostics requirement |
| WS04-12 | Implement deployment retention/pruning lifecycle controls for stale releases/artifacts | Not Started | Unassigned | Research-driven lifecycle requirement |

### WS-05 Secrets, Vault, and Credential Safety

Goal:
- Enforce secure secret handling for targets and apps via `si vault`.

Dependencies:
- WS-02, WS-03

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS05-01 | Define vault key naming conventions for PaaS | Done | Codex | Implemented convention: `PAAS__CTX_<ctx>__APP_<app>__TARGET_<target>__VAR_<name>` with deterministic segment normalization |
| WS05-02 | Implement `si paas secret` command family | Done | Codex | Added `si paas secret set|get|unset|list|key` with vault-key mapping and vault command delegation |
| WS05-03 | Prevent plaintext leakage in logs/artifacts | Done | Codex | Added deploy compose secret-literal detection with redacted diagnostics and explicit unsafe bypass, plus plaintext reveal acknowledgement guardrail for `secret get --reveal` |
| WS05-04 | Add vault trust/recipient guardrail checks in deploy flow | Done | Codex | Added deploy/rollback vault recipient + trust fingerprint preflight checks with explicit `--allow-untrusted-vault` override |
| WS05-05 | Implement context-scoped secret namespaces and vault file resolution | Not Started | Unassigned | |
| WS05-06 | Enforce no-state-in-repo and no-secret-in-output guardrails | Not Started | Unassigned | |
| WS05-07 | Implement scrubbed export/import path for non-secret metadata only | Not Started | Unassigned | |

### WS-06 Observability and Telegram Alerts

Goal:
- Deliver baseline logs, health checks, and Telegram notifications.

Dependencies:
- WS-04

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS06-01 | Implement `si paas logs` and `si paas events` | Not Started | Unassigned | |
| WS06-02 | Implement Telegram notifier setup/test/send | Not Started | Unassigned | |
| WS06-03 | Define severity policy and alert routing | Not Started | Unassigned | |
| WS06-04 | Add deploy failure and health degradation alerts | Not Started | Unassigned | |
| WS06-05 | Define audit/event log model for all `si paas` actions | Not Started | Unassigned | |
| WS06-06 | Add Telegram action hooks for operator callbacks (view logs, rollback, acknowledge) | Not Started | Unassigned | |
| WS06-07 | Add ingress/TLS (Traefik + ACME) retry observability, alerting, and operator recovery guidance | Not Started | Unassigned | Research-driven reliability requirement |

### WS-07 AI-First Automation (Codex Core)

Goal:
- Integrate Codex into daily PaaS operations workflows.

Dependencies:
- WS-02, WS-04, WS-06

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS07-01 | Define AI adapter interface and Codex implementation | Not Started | Unassigned | |
| WS07-02 | Implement `si paas ai plan` and `si paas ai inspect` | Not Started | Unassigned | |
| WS07-03 | Implement guarded `si paas ai fix` proposal mode | Not Started | Unassigned | |
| WS07-04 | Add audit trail for AI suggested/applied actions | Not Started | Unassigned | |
| WS07-05 | Define strict AI action JSON schema + validation + safety policy (proposal mode default) | Not Started | Unassigned | |

### WS-12 Stateful Agent Runtime and Event Bridge (MVP Critical for Infra Autonomy)

Goal:
- Run always-on Codex-powered infra agents that respond to crashes, failures, and scaling pressure through controlled policies.

Dependencies:
- WS-02, WS-03, WS-04, WS-06, WS-07

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS12-01 | Define incident event schema, severity taxonomy, and dedupe strategy | Not Started | Unassigned | |
| WS12-02 | Implement event bridge collectors (deploy hooks, health polls, runtime events) | Not Started | Unassigned | |
| WS12-03 | Implement context-scoped incident queue storage and retention policies | Not Started | Unassigned | |
| WS12-04 | Implement `si paas agent` command family (`enable/disable/status/logs/run-once`) | Not Started | Unassigned | |
| WS12-05 | Implement dyad-style agent runtime adapter using Codex profile auth path | Not Started | Unassigned | |
| WS12-06 | Implement remediation policy engine (`auto-allow`, `approval-required`, `deny`) | Not Started | Unassigned | |
| WS12-07 | Implement approval flow (`si paas agent approve/deny`) and Telegram callback linkage | Not Started | Unassigned | |
| WS12-08 | Implement scheduler/self-heal for agent workers (lock, health check, auto-recover) | Not Started | Unassigned | |
| WS12-09 | Add offline fake-codex and deterministic smoke tests for event-to-action loop | Not Started | Unassigned | |
| WS12-10 | Add audit artifacts per agent run and incident correlation IDs | Not Started | Unassigned | |

### WS-08 Cloud-Hosted Paid Edition

Goal:
- Deliver a simple, predictable monetized cloud model for solo-dev/solopreneur ICP.

Dependencies:
- WS-04, WS-05, WS-06

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS08-01 | Define solo-dev ICP packaging and plan matrix (self-hosted free + managed paid tiers) | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-01 |
| WS08-02 | Define and implement entitlement model using active app slots as primary billable metric | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-02 |
| WS08-03 | Implement entitlement checks at app create/deploy/scale boundaries | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-02 |
| WS08-04 | Implement Stripe checkout, billing portal, and minimal subscription webhooks | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-03/MON-04 |
| WS08-05 | Implement billing state machine (`active`, `grace`, `restricted`) and grace-period policy | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-04 |
| WS08-06 | Add CLI plan/usage visibility (`si paas cloud plan`, `si paas cloud usage`) | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-05 |
| WS08-07 | Define and implement clear upgrade/downgrade behavior with cycle-boundary downgrade | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-01 |
| WS08-08 | Implement onboarding/migration flow from self-hosted metadata to cloud account | Not Started | Unassigned | Depends on WS08-02/04 contracts |
| WS08-09 | Publish pricing, limits, and billing FAQ for solo-dev clarity | Not Started | Unassigned | Linked ticket: `paas-monetization-solo-dev.md` MON-07 |

### WS-09 Quality, Security, and Reliability

Goal:
- Establish confidence for production operations and scale.

Dependencies:
- WS-03, WS-04, WS-05, WS-06

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS09-01 | Build unit/integration/e2e test matrix | Not Started | Unassigned | |
| WS09-02 | Define failure-injection and rollback drills | Not Started | Unassigned | |
| WS09-03 | Add security review checklist and threat model | Not Started | Unassigned | |
| WS09-04 | Write ops runbook for incident response | Not Started | Unassigned | |
| WS09-05 | Add state-isolation regression tests (context boundary and leakage checks) | Not Started | Unassigned | |
| WS09-06 | Add upgrade and compatibility regression suite (arch/runtime/deploy-path coverage) | Not Started | Unassigned | Research-driven hardening requirement |

### WS-11 Dogfood State Isolation and Governance (MVP Critical)

Goal:
- Guarantee strict separation between OSS source code and private operational state.

Dependencies:
- WS-02, WS-05, WS-09

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS11-01 | Define data classification policy and allowed storage matrix | Not Started | Unassigned | Linked ticket: `paas-state-isolation-model.md` ISO-01 |
| WS11-02 | Implement context-scoped state root layout and initialization | Not Started | Unassigned | Linked ticket: `paas-state-isolation-model.md` ISO-01/ISO-03 |
| WS11-03 | Implement `si paas doctor` checks for repo-state contamination and secret exposure | Not Started | Unassigned | Linked ticket: `paas-state-isolation-model.md` ISO-04/ISO-07 |
| WS11-04 | Define backup/restore policy for private state roots and audit logs | Not Started | Unassigned | Linked ticket: `paas-state-isolation-model.md` ISO-08 |
| WS11-05 | Publish operational runbook for internal dogfood vs OSS demo contexts | Not Started | Unassigned | Linked ticket: `paas-state-isolation-model.md` ISO-08 |

### WS-10 Optional Post-MVP TUI (Deferred)

Goal:
- Evaluate and implement an optional full-screen TUI only after MVP is stable.

Dependencies:
- WS-02, WS-04, WS-06, WS-09

Work items:

| ID | Task | Status | Owner | Notes |
| --- | --- | --- | --- | --- |
| WS10-01 | Define optional TUI scope without changing core CLI contracts | Not Started | Unassigned | Deferred until after MVP |
| WS10-02 | Build read-focused dashboard view (`si paas ui`) on top of existing APIs | Not Started | Unassigned | Deferred until after MVP |
| WS10-03 | Validate AI-agent compatibility remains unaffected | Not Started | Unassigned | Deferred until after MVP |

## 8. Phase and Gate Plan

Current phase status (2026-02-17):
1. Phase A: Done.
2. Phase B: In Progress.
3. Phase C: Not Started.
4. Phase D: Not Started.
5. Phase E: Deferred post-MVP.

### Phase A (Research Baseline)

Target:
- Complete WS-01 with evidence-backed synthesis.

Exit criteria:
1. All primary competitors analyzed.
2. Feedback corpus linked and categorized.
3. Feature priority shortlist approved.

### Phase B (Terminal MVP Core)

Target:
- Complete WS-02 to WS-06 for deployable terminal MVP.

Exit criteria:
1. Multi-target SSH management works.
2. Compose deploy/rollback works.
3. Vault-backed secret workflows work.
4. Telegram alerts work.
5. Reconciler detects and reports drift.
6. Per-node ingress strategy and DNS/LB policy are implemented.
7. Parallel multi-target deploy strategies are implemented and validated.
8. Webhook-triggered deployment path is implemented with auth checks.
9. Magic-variable and add-on pack resolution is validated.
10. Context-scoped state isolation controls are implemented and pass leakage checks.
11. Deterministic deploy failure diagnostics and remediation hints are shipped.
12. TLS/ACME retry states are observable and alertable with clear operator recovery path.
13. Deployment retention/pruning lifecycle controls are implemented and validated.
14. Architecture/runtime compatibility preflight gates are enforced before rollout.

### Phase C (AI Ops Layer)

Target:
- Complete WS-07 and WS-12 for Codex-first, event-driven operations automation.

Exit criteria:
1. `si paas ai plan` and `inspect` are reliable.
2. AI actions are auditable and guarded.
3. Long-running agent workers handle failure incidents end to end.
4. Approval-gated actions require explicit operator decision.
5. Codex CLI profile-backed execution path works without direct LLM API dependency.

### Phase D (Cloud Managed Foundation)

Target:
- Complete WS-08 architecture and implementation plan, then incremental delivery.

Exit criteria:
1. Solo-dev plan matrix and pricing policy are validated.
2. Active-app-slot entitlements are enforced at deploy boundaries.
3. Stripe checkout/portal/webhooks are running in staging with tests.
4. Billing state machine and grace behavior are verified end to end.
5. CLI usage and plan visibility is clear and actionable.
6. Managed control-plane bootstrap path is defined.

### Phase E (Optional TUI Layer, Post-MVP)

Target:
- Complete WS-10 without regressing CLI automation compatibility.

Exit criteria:
1. TUI is optional and never required for operations.
2. CLI command and `--json` contracts remain stable.

## 9. Risks and Mitigations

1. Risk: Scope explosion from trying to match all competitors in MVP.
Mitigation: enforce strict MVP boundaries and phase gates.

2. Risk: SSH password operational risk.
Mitigation: password only for bootstrap; enforce migration to key auth.

3. Risk: Secret leakage in logs/artifacts.
Mitigation: vault-only secret source + redaction guardrails + review hooks.

4. Risk: AI automation making unsafe changes.
Mitigation: proposal mode by default, human confirmation for mutating ops.

5. Risk: Cloud-hosted architecture diverges from self-hosted.
Mitigation: share core deploy engine and domain models from day one.

6. Risk: TUI scope creep delays MVP and hurts automation compatibility.
Mitigation: enforce non-TUI MVP boundaries; defer WS-10 until post-MVP.

7. Risk: Parallel deploy blast radius across many targets.
Mitigation: canary-first strategies, bounded `--max-parallel`, and fast rollback path.

8. Risk: Unauthenticated webhook triggers unauthorized deployments.
Mitigation: signed webhook verification, source allowlisting, and branch/app policy checks.

9. Risk: Internal dogfood state leaks into OSS repository history.
Mitigation: context-scoped state roots outside repo + doctor checks + default refusals.

10. Risk: Monetization model is too complex for solo-dev ICP.
Mitigation: one primary billable metric, flat tiers, no overage billing in initial release.

11. Risk: Under-pricing or misaligned quotas reduce margin.
Mitigation: ship with conservative limits and review conversion/churn/usage quarterly.

12. Risk: Long-running Codex agent loops stall due to auth/session drift.
Mitigation: add agent health checks, prompt/auth detection, worker auto-recovery, and explicit operator alerts.

13. Risk: Runtime upgrades introduce hidden deploy-path regressions.
Mitigation: add compatibility preflight checks + WS09 upgrade regression suite before rollout.

14. Risk: TLS/ACME retry loops stall silently and leave apps unreachable.
Mitigation: add explicit retry state telemetry, alerting, and guided recovery commands.

15. Risk: Architecture mismatch (host/image/runtime) causes failed rollouts.
Mitigation: gate deployment on architecture/platform preflight and surface actionable fixes.

## 10. Progress Update Protocol

Every agent updating this initiative must:

1. Update workstream task statuses in:
- `tickets/paas-workstream-status-board.md`

2. Update competitor findings in:
- `tickets/paas-competitive-research-board.md`

3. Append progress entry in this file under section 11.

## 11. Progress Log

| Date | Agent | Workstream | Update | Blockers | Next |
| --- | --- | --- | --- | --- | --- |
| 2026-02-17 | Codex | WS-00 | Initial master plan created | None | Start WS-01 evidence collection |
| 2026-02-17 | Codex | WS-00 | Added stateful agent runtime architecture and WS-12 (event bridge + policy/approval flows) using Codex subscription path | None | Start WS12-01 schema + WS12-04 command scaffolding |
| 2026-02-17 | Codex | WS-00 | Fixed context-scoped state model path, locked MVP ingress to Traefik, pinned SwiftWave/Kamal upstreams, and aligned WS08/WS11 linkage notes to MON/ISO tickets | None | Start WS03-05 Traefik baseline and WS08 MON-linked implementation |
| 2026-02-17 | Codex | WS-01 | Completed Phase A research baseline: cloned/indexed primary repos, filled evidence corpus, published synthesis matrix, and approved feature shortlist | None | Begin WS-02 CLI surface implementation |
| 2026-02-17 | Codex | WS-02 | Started Phase B kickoff and moved core CLI workstream items (root command, non-interactive flags, `--json`, and context routing) to in-progress planning state | None | Execute WS02-01/02/03/06 implementation and tests |
| 2026-02-17 | Codex | WS-00 | Propagated research shortlist into implementation plan: added diagnostics, compatibility preflight, TLS retry observability, retention/pruning, and upgrade-regression tasks plus Phase B exit criteria updates | None | Execute WS03-06, WS04-11/12, WS06-07, and WS09-06 alongside core Phase B delivery |
| 2026-02-17 | Codex | WS-02 | Completed WS02-01: added `si paas` root command registration, alias coverage in root dispatch tests, and usage entry in global help output | None | Implement WS02-02 non-interactive command/flag surfaces |
| 2026-02-17 | Codex | WS-02 | Completed WS02-02: implemented non-interactive `si paas` command/flag scaffolding for all MVP command families and required subcommands | None | Implement WS02-03 stable `--json` output contracts |
| 2026-02-17 | Codex | WS-02 | Completed WS02-03: added shared machine-readable scaffold envelope (`ok`, `command`, `mode`, `fields`) and `--json` support across operational `si paas` commands | None | Implement WS02-06 context command surface and global context routing |
| 2026-02-17 | Codex | WS-02 | Completed WS02-06: wired optional interactive subcommand pickers for `si paas` command groups while preserving automation-safe behavior in non-interactive environments | None | Implement WS02-04 command contract tests and validate with CLI E2E checks |
| 2026-02-17 | Codex | WS-02 | Completed WS02-04 and WS02-05: added `paas` command contract tests plus CLI E2E verification for usage/JSON/context behaviors | Existing unrelated `tools/si` test compile failures in `codex_tmux_test.go` still block full package test run | Begin WS03-01 target model + local storage CRUD implementation |
| 2026-02-17 | Codex | WS-03 | Completed WS03-01: implemented context-scoped local target persistence and CRUD behavior for `target add/list/use/remove` with JSON + text output support | SSH/preflight and bootstrap are still pending | Implement WS03-02 SSH connectivity and preflight checks next |
| 2026-02-17 | Codex | WS-03 | Completed WS03-02: implemented live preflight execution for `si paas target check` including network reachability, SSH command execution, Docker server check, and Compose availability check | Key bootstrap path (password-to-key flow) still pending | Implement WS03-03 bootstrap path from password auth to key auth |
| 2026-02-17 | Codex | WS-03 | Completed WS03-03 and WS03-04: added password-to-key bootstrap command and upgraded `target check --all` to aggregate live health diagnostics with machine-readable output | Traefik ingress baseline and compatibility preflight are still pending | Implement WS03-05 Traefik baseline and WS03-06 compatibility preflight checks |
| 2026-02-17 | Codex | WS-03 | Completed WS03-06 by adding architecture compatibility preflights (`uname -m` normalization and `--image-platform` arch matching) to live target checks | Traefik ingress baseline (WS03-05) still pending | Implement WS03-05 Traefik baseline next, then advance WS-04 deploy engine |
| 2026-02-17 | Codex | WS-03 | Completed WS03-05 by adding Traefik ingress baseline rendering (`docker-compose.traefik.yaml`, static/dynamic config, README) plus per-target DNS/LB metadata persistence | None | Start WS-04 deploy engine and WS-05 secret workflows in parallel |
| 2026-02-17 | Codex | WS-05 | Completed WS05-01 and WS05-02 by adding standardized vault key naming and `si paas secret` command family (`set|get|unset|list|key`) wired to context/app/target namespaces | `--json` for mutating secret operations is deferred; currently supported for `secret key` and `secret list` | Proceed with WS05-03 plaintext leakage guardrails and WS04 deploy engine work |
| 2026-02-17 | Codex | WS-05 | Completed WS05-03 and WS05-04 by adding deploy plaintext-secret leakage detection/redaction, explicit plaintext reveal acknowledgement guardrail, and deploy/rollback vault trust+recipient preflight checks with an unsafe override escape hatch | Context-scoped vault mapping and export/no-secret guardrails (WS05-05..07) remain pending | Start WS04-01 release bundle/metadata scaffolding and WS04-11 deterministic failure taxonomy |
| 2026-02-17 | Codex | WS-04 | Completed WS04-01 by implementing release bundle materialization in `si paas deploy`: context-scoped bundle root, copied compose artifact, and structured `release.json` metadata with digest + guardrail snapshot | WS04-02/03 upload/apply and rollback execution are still pending | Implement WS04-02 remote upload/apply path and WS04-11 deterministic failure taxonomy next |

## 12. Immediate Next Actions

1. Implement WS04-02 remote upload and Compose apply execution path.
2. Implement WS04-11 deterministic deploy failure taxonomy and remediation output contract early in deploy engine work.
3. Implement WS04-03 health checks and rollback orchestration.
4. Implement WS06-07 TLS/ACME retry observability and alert hooks during first Traefik integration pass.
5. Implement WS04-12 retention/pruning lifecycle controls before first extended dogfood rollout.
6. Add WS09-06 upgrade/compatibility regression coverage before marking Gate B complete.
7. Implement WS05-05 context-scoped vault file resolution and namespace controls.
8. Keep MVP non-TUI boundaries and machine-readable contracts enforced in every new command.
9. Keep secondary competitor deep dives (Easypanel/Portainer/Tsuru) as ongoing background refinement, not a Phase B blocker.

## 13. Reference Links

Local project references:

1. `README.md`
2. `docs/VAULT.md`
3. `docs/DYAD.md`
4. `docs/SETTINGS.md`
5. `tools/si/root_commands.go`
6. `tools/si/subcommand_interactive.go`
7. `tools/si/dyad_interactive.go`
8. `../viva/infra/supabase/README.md`
9. `../openclaw/docs/concepts/architecture.md`
10. `../openclaw/docs/concepts/session-tool.md`
11. `tickets/paas-state-isolation-model.md`
12. `DYAD_PROTOCOL.md`
13. `agents/critic/cmd/critic/loop.go`
14. `agents/shared/docker/dyad.go`
15. `tools/si/codex_warm_weekly_reconciler.go`

External references (as of 2026-02-17):

1. Coolify docs: `https://coolify.io/docs/`
2. Coolify repo: `https://github.com/coollabsio/coolify`
3. CapRover docs: `https://caprover.com/docs/get-started.html`
4. CapRover repo: `https://github.com/caprover/caprover`
5. Dokploy docs: `https://docs.dokploy.com/docs/core`
6. Dokploy repo: `https://github.com/Dokploy/dokploy`
7. Dokku docs: `https://dokku.com/docs/getting-started/installation/`
8. Dokku repo: `https://github.com/dokku/dokku`
9. OpenClaw repo: `https://github.com/openclaw/openclaw`
10. SwiftWave repo: `https://github.com/swiftwave-org/swiftwave`
11. SwiftWave site/docs: `https://swiftwave.org/`
12. Kamal repo: `https://github.com/basecamp/kamal`
13. Kamal docs: `https://kamal-deploy.org/`
