# AI PaaS Competitive Research Board

Date created: 2026-02-17
Owner: Codex
Status: Done (Phase A)

Purpose:
- Track deep competitor discovery across repo analysis and web user sentiment.
- Provide a single update surface for multiple concurrent agents.

Status legend:
- `Not Started`
- `In Progress`
- `Blocked`
- `Done`

## 1. Platform Tracking Matrix

| Platform | Repo Cloned | Docs Reviewed | User Feedback Collected | Architecture Analyzed | Strengths Synthesized | Weaknesses Synthesized | Status | Owner | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Coolify | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Primary set complete |
| CapRover | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Primary set complete |
| Dokploy | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Primary set complete |
| Dokku | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Primary set complete |
| Easypanel | No | No | No | No | No | No | Not Started | Unassigned | Secondary set pending |
| Portainer | Yes | No | No | No | No | No | In Progress | Codex | Cloned for later adjacent baseline |
| Tsuru | Yes | No | No | No | No | No | In Progress | Codex | Cloned for later non-MVP baseline |
| SwiftWave | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Canonical upstream: `https://github.com/swiftwave-org/swiftwave` |
| Kamal | Yes | Yes | Yes | Yes | Yes | Yes | Done | Codex | Canonical upstream: `https://github.com/basecamp/kamal` |
| OpenClaw patterns | Yes | Yes | Partial | Partial | Partial | Partial | In Progress | Codex | Local repo context already loaded |

## 2. Evidence Collection Rules

Every evidence entry includes:

1. URL
2. Source type (`github_issue`, `github_discussion`, `reddit`, `hn`, `blog`, `docs`, `code`)
3. Date (absolute date)
4. Platform
5. Sentiment (`positive`, `negative`, `mixed`, `neutral`)
6. Key quote summary (paraphrased)
7. Feature/topic tag (`deploy`, `rollback`, `secrets`, `ux`, `pricing`, `stability`, `scale`, `alerts`, `ai`)
8. Confidence (`high`, `medium`, `low`)

Hard rule:
- Do not add simulated findings without a direct source URL and date.

## 3. Findings Per Platform

### Platform: Coolify

Repo:
- https://github.com/coollabsio/coolify

Docs:
- https://coolify.io/docs/installation

Strengths users repeatedly mention:
1. Fast self-host setup and broad service templates.
2. Strong SSH-first remote target model for BYO infra.
3. Active release cadence and feature requests moving quickly.

Weaknesses users repeatedly mention:
1. Some deployment failures are hard to diagnose from surfaced errors.
2. Secret handling in preview/development paths has had reliability complaints.
3. Ingress/HTTPS behavior can be surprising in edge cases.

Missing features users request:
1. Better persistent storage API controls.
2. Stronger backup/database auto-detection for compose flows.
3. More deterministic failure diagnostics in deploy pipeline.

Architecture lessons we should copy:
1. SSH-managed bring-your-own-infra control plane.
2. Template-driven app and service onboarding.
3. Tight product-doc/install-script alignment.

Architecture lessons we should avoid:
1. Low-context deploy error surfaces.
2. Secret behavior divergence between environments.
3. Ingress behavior that requires deep workaround knowledge.

### Platform: CapRover

Repo:
- https://github.com/caprover/caprover

Docs:
- https://caprover.com/docs/get-started.html

Strengths users repeatedly mention:
1. Extremely simple first deploy UX.
2. Good value/cost profile for small teams and solo developers.
3. Straightforward app+database management model.

Weaknesses users repeatedly mention:
1. Upgrade and architecture mismatch issues appear in field reports.
2. Swarm-coupled internals can constrain some workflows.
3. Modern build pipeline asks (BuildKit, richer project modeling) remain active.

Missing features users request:
1. BuildKit support as first-class build path.
2. Better project/group organization UX.
3. More explicit upgrade safety and rollback guidance.

Architecture lessons we should copy:
1. Keep onboarding and day-1 UX aggressively simple.
2. Offer both GUI and CLI surfaces for mixed operators.
3. Bake cost/value framing into docs and onboarding.

Architecture lessons we should avoid:
1. Hard runtime coupling that limits future deploy backends.
2. Upgrade paths without explicit guardrails.
3. Under-specified multi-arch deployment handling.

### Platform: Dokploy

Repo:
- https://github.com/Dokploy/dokploy

Docs:
- https://docs.dokploy.com/docs/core

Strengths users repeatedly mention:
1. Compose-native and multi-server deployment capabilities.
2. Built-in notifications and operational observability features.
3. Practical template/catalog approach for rapid deployment.

Weaknesses users repeatedly mention:
1. TLS/ACME and Traefik edge reliability issues in some recovery paths.
2. Update/restart flows can leave unhealthy states.
3. Some lifecycle operations (old deployment cleanup) require more automation.

Missing features users request:
1. More robust webhook trigger ecosystem.
2. Better deployment retention/cleanup lifecycle controls.
3. More predictable certificate retry/recovery behavior.

Architecture lessons we should copy:
1. Compose as a first-class app model.
2. Integrate notifications into deploy lifecycle events.
3. Keep multi-server management in core product surface.

Architecture lessons we should avoid:
1. ACME/ingress retry behavior that silently stalls.
2. Update flows without strong self-heal and clear failure state.
3. Resource cleanup workflows that rely on manual follow-up.

### Platform: Dokku

Repo:
- https://github.com/dokku/dokku

Docs:
- https://dokku.com/docs/getting-started/installation/

Strengths users repeatedly mention:
1. Mature lightweight self-host path.
2. Strong CLI-first automation orientation.
3. Large ecosystem and longevity in production use.

Weaknesses users repeatedly mention:
1. Plugin/runtime compatibility can regress around upgrades.
2. Zero-downtime edge cases still appear in ingress routing reports.
3. Operational complexity remains high for less-experienced users.

Missing features users request:
1. More granular persistent storage management UX.
2. Better per-app registry authentication ergonomics.
3. More reliable CNB/pack command consistency.

Architecture lessons we should copy:
1. Strong CLI/scriptability as a first-class contract.
2. Keep core runtime lean.
3. Document install/upgrade lifecycle thoroughly.

Architecture lessons we should avoid:
1. Operational workflows that depend on deep plugin-specific knowledge.
2. Routing behavior that risks zero-downtime guarantees.
3. Compatibility breaks without stronger migration safeguards.

### Platform: SwiftWave

Repo:
- https://github.com/swiftwave-org/swiftwave

Docs:
- https://swiftwave.org/docs/installation

Strengths users repeatedly mention:
1. Lightweight footprint and broad architecture support.
2. Clear focus on self-host workflows.
3. Go-based control-plane surface is approachable for infra coders.

Weaknesses users repeatedly mention:
1. Install path reliability gaps in field reports.
2. TLS/domain flow rough edges remain visible.
3. Some networking/ingress operations need stronger guardrails.

Missing features users request:
1. Built-in rate limiting and edge controls.
2. More stable guided install and post-install checks.
3. Better DNS/TLS troubleshooting workflows.

Architecture lessons we should copy:
1. Keep binary/control-plane lightweight.
2. Preserve self-host first portability.
3. Expose clear API/docs for automation consumers.

Architecture lessons we should avoid:
1. Fragile install paths without strict preflight.
2. TLS and ingress operations with ambiguous recovery behavior.
3. Networking defaults that require deep manual debugging.

### Platform: Kamal

Repo:
- https://github.com/basecamp/kamal

Docs:
- https://kamal-deploy.org/docs/installation

Strengths users repeatedly mention:
1. Clear SSH-based deploy model with strong operator mental model.
2. Zero-downtime framing is explicit in product narrative.
3. Good fit for teams wanting infra-light deployment workflow.

Weaknesses users repeatedly mention:
1. SSH reliability and handshake edge cases are visible in issues.
2. Health-check semantics can fail non-obviously during deploy.
3. Accessory/service bootstrapping has operational sharp edges.

Missing features users request:
1. Better adapter customization hooks.
2. Stronger health-check diagnostics and remediation guidance.
3. Better secrets/provider integration ergonomics.

Architecture lessons we should copy:
1. SSH transport as deterministic control path.
2. Explicit deploy/health contract and command surface.
3. Keep runtime model narrow and operationally legible.

Architecture lessons we should avoid:
1. SSH/session error flows without actionable diagnostics.
2. Health gating that fails without clear operator recovery path.
3. Provider-specific edge cases without first-class fallback behavior.

## 4. Evidence Log

| Date | Platform | Source Type | URL | Sentiment | Topic Tag | Insight Summary | Confidence | Added By |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 2026-02-17 | Coolify | docs | https://coolify.io/docs/installation | positive | ux | Install path is prominently documented and positioned as easy self-host entry. | high | Codex |
| 2026-02-17 | Coolify | code | https://github.com/coollabsio/coolify/blob/main/docker-compose.yml | positive | deploy | Compose-first deployment is visible in repository root artifacts. | high | Codex |
| 2025-11-05 | Coolify | github_issue | https://github.com/coollabsio/coolify/issues/7113 | negative | stability | Users report deploy failures with insufficiently clear root-cause output. | medium | Codex |
| 2024-08-14 | Coolify | github_issue | https://github.com/coollabsio/coolify/issues/3079 | negative | secrets | Preview deployment secret/environment behavior reported as unreliable. | medium | Codex |
| 2025-12-07 | Coolify | github_issue | https://github.com/coollabsio/coolify/issues/7528 | mixed | deploy | Users ask for deeper DB detection and backup integration for compose deployments. | medium | Codex |
| 2026-02-17 | Coolify | github_issue | https://github.com/coollabsio/coolify/issues/8397 | neutral | scale | API-level storage controls are requested for operational flexibility. | medium | Codex |
| 2026-02-17 | CapRover | docs | https://caprover.com/docs/get-started.html | positive | ux | Docs emphasize rapid setup and low-ops onboarding path. | high | Codex |
| 2026-02-17 | CapRover | code | https://github.com/caprover/caprover/blob/master/template/root-nginx-conf.ejs | positive | deploy | Nginx templating indicates opinionated ingress/runtime control in core architecture. | high | Codex |
| 2026-01-31 | CapRover | github_issue | https://github.com/caprover/caprover/issues/2373 | negative | deploy | Deployment architecture mismatch surfaced as an active user pain point. | medium | Codex |
| 2026-01-01 | CapRover | github_issue | https://github.com/caprover/caprover/issues/2366 | negative | stability | Upgrade breakage reports indicate lifecycle hardening demand. | medium | Codex |
| 2024-12-13 | CapRover | github_issue | https://github.com/caprover/caprover/issues/2225 | mixed | deploy | BuildKit support demand shows pressure for modernized build pipeline. | medium | Codex |
| 2024-10-22 | CapRover | github_issue | https://github.com/caprover/caprover/issues/2170 | neutral | ux | Users request stronger project grouping and organizational model. | medium | Codex |
| 2026-02-17 | Dokploy | docs | https://docs.dokploy.com/docs/core | positive | ux | Core docs highlight Compose and multi-server workflows as primary paths. | high | Codex |
| 2026-02-17 | Dokploy | code | https://github.com/Dokploy/dokploy/blob/canary/openapi.json | positive | deploy | API surface is explicit in repository, enabling automation-first integrations. | high | Codex |
| 2026-02-16 | Dokploy | github_issue | https://github.com/Dokploy/dokploy/issues/3724 | negative | stability | ACME/Traefik retry behavior reported to fail after DNS correction. | medium | Codex |
| 2026-02-10 | Dokploy | github_issue | https://github.com/Dokploy/dokploy/issues/3673 | negative | stability | Update-triggered unrecovered bad state reported in dashboard flow. | medium | Codex |
| 2025-09-22 | Dokploy | github_issue | https://github.com/Dokploy/dokploy/issues/2667 | mixed | deploy | Webhook-trigger expansion requests signal demand for broader CI trigger support. | medium | Codex |
| 2025-12-07 | Dokploy | github_issue | https://github.com/Dokploy/dokploy/issues/3184 | neutral | ux | Users request easier lifecycle cleanup of old deployments. | medium | Codex |
| 2026-02-17 | Dokku | docs | https://dokku.com/docs/getting-started/installation/ | positive | deploy | Install and upgrade lifecycle are heavily documented for operators. | high | Codex |
| 2026-02-17 | Dokku | code | https://github.com/dokku/dokku/tree/master/plugins | positive | scale | Plugin-oriented architecture provides broad extension surface. | high | Codex |
| 2026-01-16 | Dokku | github_issue | https://github.com/dokku/dokku/issues/8282 | negative | rollback | Traffic routing during retirement can break zero-downtime expectation. | medium | Codex |
| 2026-01-23 | Dokku | github_issue | https://github.com/dokku/dokku/issues/8302 | neutral | ux | Persistent storage management ergonomics are requested by operators. | medium | Codex |
| 2025-12-23 | Dokku | github_issue | https://github.com/dokku/dokku/issues/8242 | negative | stability | Runtime command regressions reported with CNB/pack images. | medium | Codex |
| 2022-08-23 | Dokku | github_issue | https://github.com/dokku/dokku/issues/5324 | neutral | secrets | Users ask for per-app registry login capability. | medium | Codex |
| 2026-02-17 | SwiftWave | docs | https://swiftwave.org/docs/installation | positive | ux | Project positioning stresses lightweight self-host deployment path. | high | Codex |
| 2025-09-07 | SwiftWave | github_issue | https://github.com/swiftwave-org/swiftwave/issues/1336 | negative | stability | Install warnings/failures reported in official setup flow. | medium | Codex |
| 2026-01-08 | SwiftWave | github_issue | https://github.com/swiftwave-org/swiftwave/issues/1362 | neutral | deploy | Edge rate-limiting capability requested by users. | medium | Codex |
| 2026-02-17 | Kamal | docs | https://kamal-deploy.org/docs/installation | positive | deploy | Docs emphasize SSH-based zero-downtime deployment lifecycle. | high | Codex |
| 2026-02-17 | Kamal | code | https://github.com/basecamp/kamal/blob/main/README.md | positive | deploy | Core architecture message is explicitly SSH transport with container switching. | high | Codex |
| 2024-10-03 | Kamal | github_issue | https://github.com/basecamp/kamal/issues/1041 | negative | stability | Health-target convergence failures appear in real-world deploy threads. | medium | Codex |
| 2025-08-04 | Kamal | github_issue | https://github.com/basecamp/kamal/issues/1619 | negative | deploy | SSH disconnect instability reported despite manual SSH success. | medium | Codex |
| 2026-02-17 | OpenClaw patterns | code | ../openclaw/docs/concepts/architecture.md | positive | ai | Gateway-centric control plane is clearly documented and operations-focused. | medium | Codex |
| 2026-02-17 | OpenClaw patterns | code | ../openclaw/docs/concepts/session-tool.md | positive | ai | Session tooling shows robust multi-agent orchestration and scoped access patterns. | medium | Codex |

## 5. Synthesis Summary (Phase A)

Current top themes:

1. SSH-based control remains a favored low-friction operational model for self-host operators.
2. Compose-native deployment is strongly preferred for MVP-grade simplicity.
3. Reliability pain clusters around deploy diagnostics, ingress/TLS retries, and upgrade regressions.

Current top gaps in market:

1. Deterministic, operator-readable deploy failure diagnostics are still weak across tools.
2. Context-safe secrets/state isolation is inconsistently enforced in user-facing workflows.
3. Webhook-trigger + multi-target rollout policy controls are often incomplete or brittle.

Final comparative matrix (Phase A):

| Platform | Deploy Model | Multi-Target Model | Secrets Posture | Reliability Risk Pattern |
| --- | --- | --- | --- | --- |
| Coolify | Compose + templates | SSH-managed servers | Reports of preview secret inconsistencies | Deploy error diagnosability |
| CapRover | Swarm-centric | Swarm cluster model | Functional but dated ergonomics | Upgrade and arch mismatch regressions |
| Dokploy | Compose + Traefik | Multi-node + remote servers | Broad feature set, still evolving | TLS/ACME + update recovery edge cases |
| Dokku | Git push + plugins | Plugin-driven scaling | Registry/auth feature requests remain | Zero-downtime and plugin compatibility edge cases |
| SwiftWave | Lightweight self-host | Multi-arch support | Functional, less mature tooling | Install and TLS/networking sharp edges |
| Kamal | SSH + container switch | Multi-server via SSH | Needs stronger provider/secrets ergonomics | SSH and health-check failure handling |

Approved Phase A feature shortlist for `si paas`:

1. P0: Deterministic deploy diagnostics with clear failure taxonomy and remediation hints.
2. P0: Context-scoped state and vault isolation by default with blocking safety checks.
3. P0: Multi-target rollout policies (`serial`, `rolling`, `canary`, `parallel`) with bounded blast radius.
4. P0: Webhook-triggered deploy path with strict signature validation and app/branch mapping.
5. P1: Ingress/TLS recovery guardrails and retry observability (Traefik-first in MVP).
6. P1: Retention/lifecycle cleanup controls for old deployments and artifacts.
7. P1: Explicit `--json` contracts for all operational surfaces and AI-agent-safe automation.
8. P2: Optional post-MVP UX layer on top of stable CLI contracts.

Phase A completion note:
- Primary competitor set (Coolify, CapRover, Dokploy, Dokku) analyzed with linked evidence.
- Feedback corpus is linked and categorized in section 4.
- Prioritized feature shortlist is approved in this section for Phase B execution.
