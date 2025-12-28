# Temporal migration plan (Silexa)

## Goals
- Make Temporal the system of record and orchestration layer for Silexa.
- Run Temporal self-hosted on Kubernetes to enable horizontal scaling.
- Preserve existing HTTP APIs while workflows replace hand-rolled loops.

## Target architecture (Kubernetes)
- Temporal cluster with SQL persistence (Postgres).
- Manager API as a thin gateway over Temporal state.
- Manager worker runs the state workflow.
- Future workers: router, program-manager, codex-monitor, app deployer.

## Current implementation (this repo)
- Manager stores state in a Temporal workflow (`silexa-state`) and exposes the same HTTP endpoints.
- A dedicated manager worker processes the state workflow (`silexa-state` task queue).
- Beam workflows implemented for `beam.codex_login` and `beam.dyad_bootstrap`.
- Dyad assignment policy enforcement remains in the API layer for now.
- Telegram notifications and the dyad digest use Temporal state but are still driven by the API process.

## Migration phases
1) Temporal baseline
   - Deploy Temporal on Kubernetes (see `infra/k8s/temporal`).
   - Deploy manager API + manager worker (see `infra/k8s/silexa`).
   - Validate `/healthz`, `/dyads`, `/dyad-tasks`, and Telegram notifications.
2) Router workflow
   - Convert router polling loop into a Temporal workflow + activities.
   - Replace router service with a worker deployment.
3) Codex monitor workflow
   - Wrap Codex status polling and dyad spawning in a workflow.
   - Centralize cooldown logic as Temporal state/activities.
4) Program manager workflow
   - Move program reconciliation into a workflow.
   - Use Temporal signals to open/close dyad tasks.
5) Notifications and digest
   - Move the dyad digest to a Temporal schedule.
   - Convert per-task notifications to activities with retry policies.
6) Deprecate legacy Swarm docs
   - Mark Swarm files as legacy.
   - Consolidate documentation around Kubernetes and Temporal.

## Operational checklist
- Temporal namespace exists (default or `SILEXA_NAMESPACE`).
- Manager API and worker point to the same Temporal task queue.
- Postgres backups and retention configured for Temporal visibility and default stores.
- Apply k8s resource requests/limits for manager + workers.
