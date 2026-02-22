---
title: Helia Cloud Sync
description: Use `si helia` to sync codex profiles and vault backups to the Helia cloud backend.
---

# Helia Cloud Sync

`si helia` connects SI to the Helia backend for account-scoped sync.

Primary uses:
- Sync Codex profile auth caches across machines.
- Back up encrypted SI vault files to a cloud object store.
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

## Settings keys

`[helia]` supports:
- `base_url`
- `account`
- `token`
- `timeout_seconds`
- `auto_sync`
- `vault_backup`

See [Settings](./SETTINGS) for full schema details.
