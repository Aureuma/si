# PaaS Automation Agents

SI includes three sustainable automation agents for long-running operational loops:

- `PR Guardian`: pull request triage and safe autofix lanes.
- `Website Sentry`: continuous repo/runtime health checks with remediation attempts.
- `Market Research Scout`: market signal ingestion and shared taskboard updates.

## Layout

- `tools/agents/config.sh`: runtime defaults and policy thresholds.
- `tools/agents/lib.sh`: logging, locks, retries, summary/finalization helpers.
- `tools/agents/pr-guardian.sh`: PR risk triage and bounded autofix.
- `tools/agents/website-sentry.sh`: health checks and remediation loop.
- `tools/agents/market-research-scout.sh`: market intelligence runner.
- `tools/agents/market_research_scout.py`: feed parsing, scoring, ticket generation.
- `tools/agents/doctor.sh`: preflight and syntax validation.
- `tools/agents/status.sh`: latest-run summaries.

## Reliability Patterns

- Deterministic run artifacts (`summary.md`, `run.log`, `run.jsonl`) per run.
- Filesystem lock guards to prevent overlapping runs.
- Retry policy with exponential backoff for transient failures.
- Workflow-level `concurrency` guards in GitHub Actions.

## Workflows

- `.github/workflows/agent_pr_guardian.yml`
- `.github/workflows/agent_website_sentry.yml`
- `.github/workflows/agent_market_research.yml`

Artifacts are stored under `.artifacts/agent-logs/`.

## Local Operations

```bash
bash tools/agents/doctor.sh
bash tools/agents/status.sh
bash tools/agents/pr-guardian.sh
bash tools/agents/website-sentry.sh
bash tools/agents/market-research-scout.sh
```

## Notes

- `pr-guardian` only pushes autofixes on same-repo pull request events.
- `website-sentry` runs SI test workflows and attempts safe formatting remediation.
- `market-research-scout` updates `tickets/taskboard/` and `tickets/market-research/`.
