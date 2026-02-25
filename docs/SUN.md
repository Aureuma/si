---
title: Sun Cloud Sync
description: Use `si sun` to authenticate and operate Sun-backed SI workflows.
---

# Sun Cloud Sync

`si sun` connects SI to the Sun backend for account-scoped data and control APIs.

Primary uses:
- authenticate SI (`si sun auth ...`)
- sync codex profiles (`si sun profile ...`)
- power remote vault operations (`si vault ...` with Sun backend)
- manage tokens, audit, taskboard, and machine control

## Auth

```bash
si sun auth login --url <sun-url> --token <token> --account <slug> --auto-sync
si sun auth status
si sun doctor
```

Google browser flow (optional):

```bash
si sun auth login --google --login-url https://aureuma.ai/sun/auth/cli/start --timeout-seconds 180
```

## Profiles

```bash
si sun profile list
si sun profile push --profile <id>
si sun profile pull --profile <id>
```

## Vault in Sun Mode

Vault is cloud-first and scope-based.

Use `si vault` directly:

```bash
si vault init --scope default --set-default
si vault set MY_KEY --stdin --scope default
si vault get MY_KEY --scope default --reveal
si vault list --scope default
si vault history MY_KEY --scope default --json
si vault run --scope default -- ./cmd
si vault docker exec --scope default --container <id> -- ./cmd
```

Status/debug:

```bash
si vault backend status
si vault sync status --scope default --json
```

Notes:
- `si vault sync push/pull` is unsupported in remote mode.
- Legacy backup push/pull flows are superseded by direct Sun KV operations.
- Trust is Sun-managed in remote mode.

## Tokens and Audit

```bash
si sun token list
si sun token create --label laptop --scopes objects:read,objects:write --expires-hours 720
si sun token revoke --token-id <id>
si sun audit list --limit 20
```

## Taskboard

```bash
si sun taskboard use --name shared --agent dyad:main
si sun taskboard add --name shared --title "T1" --prompt "Do work" --priority P1
si sun taskboard claim --name shared --agent dyad:main
si sun taskboard done --name shared --id <task-id> --agent dyad:main --result "done"
```

## Machine Control

```bash
si sun machine register --machine controller-a --operator op:controller@local --can-control-others --can-be-controlled=false
si sun machine register --machine worker-a --operator op:worker@remote --allow-operators op:controller@local --can-be-controlled
si sun machine run --machine worker-a --source-machine controller-a --operator op:controller@local --wait -- version
si sun machine serve --machine worker-a
```

Boundary:
- `si sun machine ...` = remote command/control plane
- `si paas ...` = app/platform workflows
