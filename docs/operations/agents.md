# Automation Agents

This repository includes two sustainable automation agents:

- `PR Guardian` for pull-request triage and safe autofixes.
- `Website Sentry` for website health monitoring, remediation, and escalation.

## Layout

- `tools/agents/config.sh`: shared runtime defaults and policy thresholds.
- `tools/agents/lib.sh`: logging, lock control, retries, and run finalization.
- `tools/agents/pr-guardian.sh`: PR triage + autofix flow.
- `tools/agents/website-sentry.sh`: health + remediation flow.
- `tools/agents/doctor.sh`: preflight/syntax validation for the agent system.
- `tools/agents/status.sh`: latest-run summary for all agents.

## Reliability Patterns

1. Deterministic run artifacts
- Every run writes a dedicated timestamped folder with `summary.md`, `run.log`, and `run.jsonl`.

2. Locking to avoid overlapping automation
- Agents acquire filesystem locks under `tmp/agent-locks/`.
- Concurrent runs are skipped safely with explicit status reporting.

3. Retry policy for transient failures
- Health checks use bounded retries with exponential backoff.
- Retry behavior is centralized in `lib.sh` and configured in `config.sh`.

4. Workflow concurrency guardrails
- GitHub workflows define explicit `concurrency` groups.
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

## Logging Contract

Per-run files:
- `summary.md`: operator-readable run report.
- `run.log`: line-oriented command execution log.
- `run.jsonl`: structured log lines for parsing/automation.

Location:
- `.artifacts/agent-logs/pr-guardian/<timestamp-pid>/`
- `.artifacts/agent-logs/website-sentry/<timestamp-pid>/`

Retention:
- Old run folders are garbage-collected automatically (default: 14 days).

## Operations

Run locally:

```bash
bash tools/agents/doctor.sh
bash tools/agents/status.sh
bash tools/agents/pr-guardian.sh
bash tools/agents/website-sentry.sh
```

Notes:
- `pr-guardian` is intended for GitHub Actions PR events.
- `website-sentry` supports both scheduled and manual workflow dispatch.
