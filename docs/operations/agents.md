# Automation Agents

This repository includes three sustainable automation agents:

- `PR Guardian` for pull-request triage and safe autofixes.
- `Website Sentry` for website health monitoring, remediation, and escalation.
- `Market Research Scout` for opportunity discovery and PaaS task planning.

The implementation is organized for operational durability and explicitly adopts
reliability patterns inspired by OpenClaw automation docs/workflows.

## Layout

- `scripts/agents/config.sh`: shared runtime defaults and policy thresholds.
- `scripts/agents/lib.sh`: logging, lock control, retries, and run finalization.
- `scripts/agents/pr-guardian.sh`: PR triage + autofix flow.
- `scripts/agents/website-sentry.sh`: health + remediation flow.
- `scripts/agents/market-research-scout.sh`: market intelligence and taskboard sync.
- `scripts/agents/doctor.sh`: preflight/syntax validation for the agent system.
- `scripts/agents/status.sh`: latest-run summary for all agents.

## OpenClaw Learnings Applied

1. Deterministic run artifacts
- Every run writes a dedicated timestamped folder with `summary.md`, `run.log`,
  and `run.jsonl`.
- This mirrors OpenClaw’s “explicit run state + inspectability” approach.

2. Locking to avoid overlapping automation
- Agents acquire filesystem locks under `tmp/agent-locks/`.
- Concurrent runs are skipped safely with explicit status reporting.

3. Retry policy for transient failures
- Health checks use bounded retries with exponential backoff.
- Retry behavior is centralized in `lib.sh` and configured in `config.sh`.

4. Command ladder / troubleshooting workflow
- Introduced `pnpm run agent:doctor` and `pnpm run agent:status` to provide a
  fast operator ladder, similar to OpenClaw’s automation troubleshooting flow.

5. Workflow concurrency guardrails
- GitHub workflows now define explicit `concurrency` groups.
- This reduces duplicated or conflicting remediation runs.

## Agent 1: PR Guardian

Workflow: `.github/workflows/agent_pr_guardian.yml`

Responsibilities:
- Diff-aware risk triage (`low|medium|high`) with policy thresholds.
- Safe formatting autofixes only (non-destructive scope).
- Pushes fixes only to same-repo PR branches.
- Applies labels and maintains sticky PR report comments.
- Uploads artifacts and writes job summaries.

## Agent 2: Website Sentry

Workflow: `.github/workflows/agent_website_sentry.yml`

Responsibilities:
- Executes website health suite (`check`, `test`, `build`) for `app/rm`.
- If failures occur, executes remediation cycles and re-checks.
- Opens remediation PR when recovered with source changes.
- Opens/updates failure issue when remediation is exhausted.
- Uploads artifacts and writes job summaries.

## Agent 3: Market Research Scout

Workflow: `.github/workflows/agent_market_research.yml`

Responsibilities:
- Scans curated external RSS feeds for high-signal market changes.
- Scores opportunities against ReleaseMind + PaaS strategy keywords.
- Produces actionable plans + 7-day experiments per opportunity.
- Creates/updates shared taskboard entries and market-research tickets.
- Generates dated reports under `docs/market-research/opportunities/`.
- Opens an automated PR when new tasks are discovered.

## Logging Contract

Per-run files:
- `summary.md`: operator-readable run report.
- `run.log`: line-oriented command execution log.
- `run.jsonl`: structured log lines for parsing/automation.

Location:
- `artifacts/agent-logs/pr-guardian/<timestamp-pid>/`
- `artifacts/agent-logs/website-sentry/<timestamp-pid>/`
- `artifacts/agent-logs/market-research-scout/<timestamp-pid>/`

Retention:
- old run folders are garbage-collected automatically (default: 14 days).

## Operations

Run locally:

```bash
pnpm run agent:doctor
pnpm run agent:status
pnpm run agent:pr-guardian
pnpm run agent:website-sentry
pnpm run agent:market-research
```

Notes:
- `pr-guardian` is intended for GitHub Actions PR events.
- `website-sentry` supports both scheduled and manual workflow dispatch.
