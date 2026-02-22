---
title: PaaS Overview
description: Structured guide to SI PaaS command surfaces, runbooks, reliability controls, and operational documentation.
---

# PaaS Overview

`si paas` is the platform command surface for operating applications, environments, backups, events, and automation on SI PaaS.

Boundary with `si sun machine`:
- `si paas` is the app/platform control plane.
- `si sun machine` is generic host-level remote command orchestration with ACL.
- Use `si sun machine run ... -- paas ...` only when you intentionally need to execute PaaS workflows from another machine.

## Command surface

```bash
si paas [--context <name>] <target|app|deploy|rollback|logs|alert|secret|ai|context|doctor|agent|events|backup|taskboard> [args...]
```

## Core operator flows

### 1. Validate platform state

```bash
si paas doctor --json
si paas context list --json
si paas target list --json
```

### 2. Deploy and observe

```bash
si paas deploy --app <slug> --json
si paas logs --app <slug> --tail 200 --json
si paas events tail --app <slug> --json
```

### 3. Backup and restore readiness

```bash
si paas backup run --app <slug> --json
si paas backup list --app <slug> --json
```

### 4. Agent automation lifecycle

```bash
si paas agent list --json
si paas agent run-once --name <agent> --json
si paas agent logs --name <agent> --tail 50 --json
```

## Documentation map

### Platform core

- [PaaS SSH Transport Architecture](./PAAS_SSH_TRANSPORT_ARCHITECTURE)
- [PaaS Test Matrix](./PAAS_TEST_MATRIX)
- [PaaS Context Operations Runbook](./PAAS_CONTEXT_OPERATIONS_RUNBOOK)
- [PaaS Automation Agents](./PAAS_AUTOMATION_AGENTS)
- [PaaS Backup and Restore Policy](./PAAS_BACKUP_RESTORE_POLICY)

### Agent runtime and approvals

- [PaaS Agent Runtime Commands](./PAAS_AGENT_RUNTIME_COMMANDS)
- [PaaS Agent Runtime Adapter](./PAAS_AGENT_RUNTIME_ADAPTER)
- [PaaS Agent Approval Flow](./PAAS_AGENT_APPROVAL_FLOW)
- [PaaS Agent Offline Smoke Tests](./PAAS_AGENT_OFFLINE_SMOKE_TESTS)
- [PaaS Agent Scheduler Self-Heal](./PAAS_AGENT_SCHEDULER_SELF_HEAL)
- [PaaS Agent Audit Artifacts](./PAAS_AGENT_AUDIT_ARTIFACTS)

### Reliability, incidents, and policies

- [PaaS Incident Runbook](./PAAS_INCIDENT_RUNBOOK)
- [PaaS Incident Event Schema](./PAAS_INCIDENT_EVENT_SCHEMA)
- [PaaS Incident Queue Policy](./PAAS_INCIDENT_QUEUE_POLICY)
- [PaaS Failure Drills](./PAAS_FAILURE_DRILLS)
- [PaaS Remediation Policy Engine](./PAAS_REMEDIATION_POLICY_ENGINE)
- [PaaS State Classification Policy](./PAAS_STATE_CLASSIFICATION_POLICY)
- [PaaS Security Threat Model](./PAAS_SECURITY_THREAT_MODEL)
- [PaaS Event Bridge Collectors](./PAAS_EVENT_BRIDGE_COLLECTORS)

## Operational guardrails

- Always run `si paas doctor` before production writes.
- Use `--json` in automation paths for stable parsing.
- Keep backup cadence and restore drills aligned with the backup policy.
- Use approval and audit flows for any high-impact remediations.
