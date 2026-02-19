# PaaS Automation Agents

SI includes two sustainable automation agents for long-running operational loops:

- `PR Guardian`: pull request triage and safe autofix lanes.
- `Website Sentry`: continuous repo/runtime health checks with remediation attempts.

## Layout

- `tools/agents/config.sh`: runtime defaults and policy thresholds.
- `tools/agents/lib.sh`: logging, locks, retries, summary/finalization helpers.
- `tools/agents/pr-guardian.sh`: PR risk triage and bounded autofix.
- `tools/agents/website-sentry.sh`: health checks and remediation loop.
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

Artifacts are stored under `.artifacts/agent-logs/`.

## Local Operations

```bash
bash tools/agents/doctor.sh
bash tools/agents/status.sh
bash tools/agents/pr-guardian.sh
bash tools/agents/website-sentry.sh
```

## Notes

- `pr-guardian` only pushes autofixes on same-repo pull request events.
- `website-sentry` runs SI test workflows and attempts safe formatting remediation.
