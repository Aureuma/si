---
title: Sun Cloud Sync
description: Use `si sun` to sync codex profiles and vault backups to the Sun cloud backend.
---

# Sun Cloud Sync

`si sun` connects SI to the Sun backend for account-scoped sync.

Primary uses:
- Sync Codex profile auth caches across machines.
- Back up encrypted SI vault files to a cloud object store.
- Share a Sun-backed dyad taskboard across machines/agents.
- Manage Sun device tokens and inspect audit history.

When `si sun auth login` succeeds, SI sets `vault.sync_backend = "sun"` for that machine so vault operations default to Sun-backed behavior.

## Prerequisites

- Reachable Sun API URL (for example `http://127.0.0.1:8080`).
- Sun bearer token with at least:
  - `objects:read`
  - `objects:write`
- Optional for integration gateway endpoints:
  - `integrations:read`
  - `integrations:write`
- Optional for token management:
  - `tokens:read`
  - `tokens:write`
- Optional for audit listing:
  - `audit:read`
- Optional for Sun dashboard dyad control endpoints:
  - `dyads:read`
  - `dyads:write`

## Initial auth

```bash
si sun auth login \
  --url http://127.0.0.1:8080 \
  --token "$SI_SUN_TOKEN" \
  --account acme \
  --auto-sync
```

Google browser flow (Aureuma auth broker):

```bash
si sun auth login \
  --google \
  --login-url https://aureuma.ai/sun/auth/cli/start \
  --timeout-seconds 180 \
  --auto-sync
```

This flow starts a loopback callback listener on `127.0.0.1`, opens your browser,
and stores the returned Sun token in local SI settings.
For headless/custom launchers, set `SI_SUN_LOGIN_OPEN_CMD` (supports `{url}` placeholder).

Security note:
- Use `https://` Sun URLs for any non-local deployment.
- `http://` is accepted only for loopback (`localhost`, `127.0.0.1`, `::1`) unless `SI_SUN_ALLOW_INSECURE_HTTP=1` is set.

Verify:

```bash
si sun auth status
si sun doctor
```

## Codex profile sync

Push one profile:

```bash
si sun profile push --profile america
```

Push all configured profiles:

```bash
si sun profile push
```

Pull one profile:

```bash
si sun profile pull --profile america
```

Pull all cloud profiles:

```bash
si sun profile pull
```

List cloud profile objects:

```bash
si sun profile list
```

## Vault backup sync

Push:

```bash
si sun vault backup push --file ~/.si/vault/.env --name default
```

Pull:

```bash
si sun vault backup pull --file ~/.si/vault/.env --name default
```

Equivalent vault-namespace commands:

```bash
si vault sync status --file ~/.si/vault/.env --name default
si vault sync push --file ~/.si/vault/.env --name default
si vault sync pull --file ~/.si/vault/.env --name default
```

`si sun vault backup push` also mirrors each key to Sun KV objects (`vault_kv.<scope>/<KEY>`) so vault reads can use direct cloud key state.

Inspect per-key cloud revision history:

```bash
si vault history <KEY> [--file <path>] [--limit <n>] [--json]
```

Select vault backend mode:

```bash
si vault backend use --mode sun   # Sun backup required on mutating vault commands (only supported mode)
si vault backend status
```

When vault backend is `sun`, `si vault init|set|unset|fmt|encrypt|recipients add|recipients remove` perform automatic backup behavior for the configured `sun.vault_backup` object.
In `sun` mode, SI also auto-hydrates local vault state and vault identity material from Sun before vault commands run.
In `sun` mode, new encrypted values created by `si vault set` and plaintext `si vault encrypt` are encrypted to the Sun vault identity recipient (existing legacy ciphertext remains unchanged).
In `sun` mode, `si vault get`, `si vault list`, and `si vault run` prefer Sun KV key reads when available, then fall back to the local hydrated file.

Security rule:
- Auto-backup skips vault files that contain plaintext keys.
- Use `si vault encrypt --file <path>` before backup if plaintext keys are detected.
- In `sun` backend mode, plaintext vault files are treated as errors for mutating commands until encrypted.

## Token and audit workflows

```bash
si sun token list
si sun token create --label laptop --scopes objects:read,objects:write --expires-hours 720
si sun token revoke --token-id <id>
si sun audit list --kind codex_profile_bundle --limit 20
si sun audit list --kind vault_backup --limit 20
```

## Shared dyad taskboard

Set a default board (and optional default agent id):

```bash
si sun taskboard use --name shared --agent dyad:main-laptop
```

Add work items (include `--prompt` for dyad autopilot seed text):

```bash
si sun taskboard add --title "Harden release workflow" --prompt "Audit and fix release asset upload flow" --priority P1 --name shared
si sun taskboard list --name shared
```

Claim/release/complete with optimistic lock ownership:

```bash
si sun taskboard claim --name shared --agent dyad:main-laptop
si sun taskboard release --name shared --id <task-id> --agent dyad:main-laptop
si sun taskboard done --name shared --id <task-id> --agent dyad:main-laptop --result "merged and verified"
```

Dyad autopilot integration:

```bash
# If --prompt is omitted, autopilot claims from sun.taskboard and uses task.prompt.
si dyad spawn release-bot --autopilot --profile main
```

## Plugin Gateway Registry

Build and push a sharded integration registry to Sun:

```bash
si plugins gateway push --source ./integrations --registry global --slots 32
```

Pull a filtered local catalog from Sun:

```bash
si plugins gateway pull --registry global --namespace acme --capability chat.send
```

Inspect registry metadata:

```bash
si plugins gateway status --registry global
```

## Cross-machine SI control

`si sun machine` provides generic host-level remote SI execution over Sun objects.

Boundary with `si paas`:
- `si sun machine ...`: machine orchestration and ACL for running arbitrary `si` commands remotely.
- `si paas ...`: app/platform control plane workflows (targets, deploy, logs, backup, agent).
- If needed, dispatch a remote PaaS command via `si sun machine run ... -- paas ...`.

Register a controller machine and a worker machine:

```bash
si sun machine register \
  --machine controller-a \
  --operator op:controller@local \
  --can-control-others \
  --can-be-controlled=false

si sun machine register \
  --machine worker-a \
  --operator op:worker@remote \
  --allow-operators op:controller@local \
  --can-be-controlled
```

Dispatch and execute a remote command:

```bash
# Dispatch from controller:
si sun machine run \
  --machine worker-a \
  --source-machine controller-a \
  --operator op:controller@local \
  --wait \
  -- version

# Run worker loop on worker machine:
si sun machine serve --machine worker-a
```

`--wait` behavior contract:
- Exits `0` only when the remote job reaches `succeeded`.
- Exits non-zero when the remote job reaches `failed` or `denied`.
- With `--json`, prints the terminal job JSON to stdout before exiting, so automation can both parse result data and rely on process exit code.

ACL updates (owner-only):

```bash
si sun machine allow --machine worker-a --grant op:ci@runner --as op:worker@remote
si sun machine deny --machine worker-a --revoke op:ci@runner --as op:worker@remote
```

## Settings keys

`[sun]` supports:
- `base_url`
- `account`
- `token`
- `timeout_seconds`
- `auto_sync`
- `vault_backup`
- `plugin_gateway_registry`
- `plugin_gateway_slots`
- `taskboard`
- `taskboard_agent`
- `taskboard_lease_seconds`
- `machine_id`
- `operator_id`

See [Settings](./SETTINGS) for full schema details.
Legacy compatibility: `SI_SUN_*` environment variables are still accepted.
