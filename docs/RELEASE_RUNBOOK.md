---
title: Release Runbook
description: ReleaseMind runbook commands and release-day evidence workflow.
---

# Release Runbook

SI uses ReleaseMind runbooks to keep release-day evidence visible across the
dashboard, CLI, and deployment workflow. The runbook is stored by ReleaseMind;
SI orbit is the operator command surface for reading the plan and marking gates
complete from terminal-driven work.

## Commands

```bash
si orbit releasemind runbook plan --repo Aureuma/si --json
si orbit releasemind runbook status --repo Aureuma/si <post-id> --json
si orbit releasemind runbook complete --repo Aureuma/si <post-id> tests --evidence "cargo test -p si-rs-cli releasemind_runbook"
```

If `--repo` is omitted, SI infers `owner/repo` from the current GitHub origin.
Runbook commands use the saved ReleaseMind CLI session from
`si orbit releasemind auth login`.

## Standard Gates

ReleaseMind's default GitHub release template currently tracks:

- `version_bump`
- `tests`
- `docs`
- `approval`
- `github_draft`
- `publish`
- `post_publish_verification`

The dashboard can complete `github_draft` and `publish` automatically after
successful GitHub actions. SI orbit should complete the other gates when the
operator has evidence from local validation, review, deployment, or
post-publish checks.

## Release Workflow

1. Request the plan before release work:

```bash
si orbit releasemind runbook plan --repo Aureuma/si
```

2. Complete local gates with concrete evidence:

```bash
si orbit releasemind runbook complete --repo Aureuma/si <post-id> version_bump --evidence "commit <sha> bumped patch version"
si orbit releasemind runbook complete --repo Aureuma/si <post-id> tests --evidence "cargo test -p si-rs-cli releasemind_runbook"
si orbit releasemind runbook complete --repo Aureuma/si <post-id> docs --evidence "docs updated in commit <sha>"
si orbit releasemind runbook complete --repo Aureuma/si <post-id> approval --evidence "reviewed by release operator"
```

3. Use ReleaseMind to create/update the GitHub draft and publish the release.

4. Complete post-publish verification:

```bash
si orbit releasemind runbook complete --repo Aureuma/si <post-id> post_publish_verification --evidence "verified GitHub release URL and deployed route"
```

## Secret Boundary

Runbook evidence must not contain secrets. For deployment or infrastructure
steps, resolve credentials through `si fort` and record only command evidence,
release URLs, commit SHAs, or verification links in ReleaseMind.
