# PaaS SSH Transport Architecture

This document captures lessons from SSH-first deployment tools and defines the `si paas` transport model.

## Why this exists

`si paas` already orchestrates fanout deploys, health checks, rollback, and blue/green. The transport layer historically depended on local shell binaries (`ssh`, `scp`, `sshpass`).

To reduce machine dependencies and make behavior deterministic across macOS/Linux runners, `si paas` now moves to a native Go SSH transport by default, with explicit compatibility fallback.

## Comparative study (SSH-first tools)

### Kamal (Ruby)

Strengths:
- Very practical SSH-first deploy UX.
- Registry login + container lifecycle + rollback as one flow.
- No cluster control plane required.

Weaknesses:
- Host scheduling and placement are intentionally minimal.
- Cross-node reconciliation is limited compared to orchestrators.

Takeaway for SI:
- Keep deploy ergonomics simple, but keep explicit health and rollback state in SI stores.

### Capistrano (Ruby)

Strengths:
- Predictable release directory model (`releases`, `current`).
- Strong rollback via symlink switching.
- Clear task graph semantics.

Weaknesses:
- Requires custom tasks for modern container-native patterns.
- Parallelism is coarse and plugin-driven.

Takeaway for SI:
- Preserve deterministic release IDs and history, with explicit rollback targets.

### Fabric (Python)

Strengths:
- Lightweight remote execution, excellent for procedural ops.
- Low conceptual overhead.

Weaknesses:
- Idempotency and drift semantics are user-defined.
- Hard to scale safely without additional structure.

Takeaway for SI:
- Keep a small transport surface, but pair it with policy/state/audit layers.

### pyinfra (Python)

Strengths:
- Declarative operation model over SSH with idempotency focus.
- Good mixed-fleet operation patterns.

Weaknesses:
- General-purpose state system adds abstraction for simple deploy cases.

Takeaway for SI:
- Capture idempotent checks where needed, but avoid adding a full config-management DSL inside SI.

### Ansible (Python)

Strengths:
- Mature SSH transport and inventory model.
- Very broad module ecosystem.

Weaknesses:
- Runtime/model complexity can be heavy for direct application deploy flow.
- Execution behavior can become opaque in large playbooks.

Takeaway for SI:
- Keep target inventory simple and explicit. Prefer typed SI commands over free-form playbook logic.

### Deployer (PHP)

Strengths:
- Clean task-oriented SSH deploy model.
- Strong for app-focused release pipelines.

Weaknesses:
- Ecosystem scope smaller than broader ops tools.

Takeaway for SI:
- Keep task-level composability while retaining first-class deploy primitives.

### Dokku (Shell + Go components)

Strengths:
- PaaS-like UX on raw VPS, low control-plane overhead.
- Operational simplicity for single/mid-size fleets.

Weaknesses:
- Multi-node control and advanced scheduling are not primary goals.

Takeaway for SI:
- Focus SI on execution and integration consistency, not full scheduler replacement.

## SI transport design principles

1. Default to native Go SSH transport for deterministic behavior.
2. Preserve `exec` fallback for compatibility and test harnesses.
3. Keep TOFU host-key behavior explicit and auditable.
4. Support key and password auth without mandatory external binaries.
5. Keep transport separate from rollout policy logic (fanout/bluegreen/rollback).

## Engine model

`SI_PAAS_SSH_ENGINE` values:
- `auto` (default): use Go engine unless legacy SSH/SCP binary overrides are set.
- `go`: force native Go SSH transport.
- `exec`: force shell-based `ssh`/`scp` behavior.

Compatibility env keys remain supported:
- `SI_PAAS_SSH_BIN`
- `SI_PAAS_SCP_BIN`
- `SI_PAAS_SSHPASS_BIN`

New auth/host policy env keys:
- `SI_PAAS_SSH_PASSWORD`
- `SI_PAAS_SSH_PRIVATE_KEY`
- `SI_PAAS_KNOWN_HOSTS_FILE`

## Scope boundaries

Native transport covers:
- Remote command execution.
- Bundle upload.
- Password bootstrap path for key installation.

Out of scope:
- Replacing external schedulers (Nomad/K8s/Swarm).
- Stateful control-plane consensus for cluster membership.

SI stays execution-centric while integrating with commodity schedulers where needed.
