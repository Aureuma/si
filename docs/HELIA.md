---
title: Helia Cloud Sync
description: Use `si helia` to sync codex profiles and vault backups to the Helia cloud backend.
---

# Helia Cloud Sync

`si helia` connects SI to the Helia backend for account-scoped sync.

Primary uses:
- Sync Codex profile auth caches across machines.
- Back up encrypted SI vault files to a cloud object store.
- Share a Helia-backed dyad taskboard across machines/agents.
- Manage Helia device tokens and inspect audit history.

## Prerequisites

- Reachable Helia API URL (for example `http://127.0.0.1:8080`).
- Helia bearer token with at least:
  - `objects:read`
  - `objects:write`
- Optional for token management:
  - `tokens:read`
  - `tokens:write`
- Optional for audit listing:
  - `audit:read`

## Initial auth

```bash
si helia auth login \
  --url http://127.0.0.1:8080 \
  --token "$SI_HELIA_TOKEN" \
  --account acme \
  --auto-sync
```

Security note:
- Use `https://` Helia URLs for any non-local deployment.
- `http://` is accepted only for loopback (`localhost`, `127.0.0.1`, `::1`) unless `SI_HELIA_ALLOW_INSECURE_HTTP=1` is set.

Verify:

```bash
si helia auth status
si helia doctor
```

## Codex profile sync

Push one profile:

```bash
si helia profile push --profile america
```

Push all configured profiles:

```bash
si helia profile push
```

Pull one profile:

```bash
si helia profile pull --profile america
```

Pull all cloud profiles:

```bash
si helia profile pull
```

List cloud profile objects:

```bash
si helia profile list
```

## Vault backup sync

Push:

```bash
si helia vault backup push --file ~/.si/vault/.env --name default
```

Pull:

```bash
si helia vault backup pull --file ~/.si/vault/.env --name default
```

Select vault backend mode:

```bash
si vault backend use --mode git   # local/git-only
si vault backend use --mode dual  # local/git + best-effort Helia backup
si vault backend use --mode helia # Helia backup required on mutating vault commands
si vault backend status
```

When vault backend is `dual` or `helia`, `si vault init|set|unset|fmt|encrypt` perform automatic backup behavior for the configured `helia.vault_backup` object.

Security rule:
- Auto-backup skips vault files that contain plaintext keys.
- Use `si vault encrypt --file <path>` before backup if plaintext keys are detected.
- In `helia` backend mode, plaintext vault files are treated as errors for mutating commands until encrypted.

## Token and audit workflows

```bash
si helia token list
si helia token create --label laptop --scopes objects:read,objects:write --expires-hours 720
si helia token revoke --token-id <id>
si helia audit list --kind codex_profile_bundle --limit 20
si helia audit list --kind vault_backup --limit 20
```

## Shared dyad taskboard

Set a default board (and optional default agent id):

```bash
si helia taskboard use --name shared --agent dyad:main-laptop
```

Add work items (include `--prompt` for dyad autopilot seed text):

```bash
si helia taskboard add --title "Harden release workflow" --prompt "Audit and fix release asset upload flow" --priority P1 --name shared
si helia taskboard list --name shared
```

Claim/release/complete with optimistic lock ownership:

```bash
si helia taskboard claim --name shared --agent dyad:main-laptop
si helia taskboard release --name shared --id <task-id> --agent dyad:main-laptop
si helia taskboard done --name shared --id <task-id> --agent dyad:main-laptop --result "merged and verified"
```

Dyad autopilot integration:

```bash
# If --prompt is omitted, autopilot claims from helia.taskboard and uses task.prompt.
si dyad spawn release-bot --autopilot --profile main
```

## Cross-machine SI control

`si helia machine` provides generic host-level remote SI execution over Helia objects.

Boundary with `si paas`:
- `si helia machine ...`: machine orchestration and ACL for running arbitrary `si` commands remotely.
- `si paas ...`: app/platform control plane workflows (targets, deploy, logs, backup, agent).
- If needed, dispatch a remote PaaS command via `si helia machine run ... -- paas ...`.

Register a controller machine and a worker machine:

```bash
si helia machine register \
  --machine controller-a \
  --operator op:controller@local \
  --can-control-others \
  --can-be-controlled=false

si helia machine register \
  --machine worker-a \
  --operator op:worker@remote \
  --allow-operators op:controller@local \
  --can-be-controlled
```

Dispatch and execute a remote command:

```bash
# Dispatch from controller:
si helia machine run \
  --machine worker-a \
  --source-machine controller-a \
  --operator op:controller@local \
  --wait \
  -- version

# Run worker loop on worker machine:
si helia machine serve --machine worker-a
```

`--wait` behavior contract:
- Exits `0` only when the remote job reaches `succeeded`.
- Exits non-zero when the remote job reaches `failed` or `denied`.
- With `--json`, prints the terminal job JSON to stdout before exiting, so automation can both parse result data and rely on process exit code.

ACL updates (owner-only):

```bash
si helia machine allow --machine worker-a --grant op:ci@runner --as op:worker@remote
si helia machine deny --machine worker-a --revoke op:ci@runner --as op:worker@remote
```

## Settings keys

`[helia]` supports:
- `base_url`
- `account`
- `token`
- `timeout_seconds`
- `auto_sync`
- `vault_backup`
- `taskboard`
- `taskboard_agent`
- `taskboard_lease_seconds`
- `machine_id`
- `operator_id`

See [Settings](./SETTINGS) for full schema details.
