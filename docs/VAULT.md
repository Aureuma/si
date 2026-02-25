# `si vault` (Sun Remote Vault)

`si vault` is now Sun-backed by default. Secrets are read/written through Sun APIs for every operation.

Core properties:
- no local trust file management
- no local vault backup file hydration/pull/push
- no local private key file required in normal flow
- per-scope cloud key storage and revision history
- runtime env injection for shells, Docker, and any program (`go run`, binaries, scripts)

## Mental Model

- A **scope** is a logical vault namespace (for example: `default`, `prod`, `billing`).
- Commands target one scope at a time.
- Use `--scope <name>` (preferred) or `--file <name>` (compat alias).
- Default scope comes from `vault.file` (default: `default`).

## Prerequisites

Authenticate once:

```bash
si sun auth login --url <sun-url> --token <token> --account <slug>
```

Verify:

```bash
si sun auth status
si vault status
```

## Quickstart

Initialize scope + ensure cloud identity:

```bash
si vault init --scope default --set-default
```

Set secret:

```bash
si vault set OPENAI_API_KEY --stdin --scope default
```

Get metadata (no plaintext):

```bash
si vault get OPENAI_API_KEY --scope default
```

Reveal plaintext:

```bash
si vault get OPENAI_API_KEY --scope default --reveal
```

List keys:

```bash
si vault list --scope default
```

Unset key:

```bash
si vault unset OPENAI_API_KEY --scope default
```

History:

```bash
si vault history OPENAI_API_KEY --scope default --limit 20
```

Cloud encryption check and remediation:

```bash
si vault check --scope default
si vault encrypt --scope default
```

## Runtime Injection

Shell/scripts/Go:

```bash
si vault run --scope default -- go run ./cmd/server
si vault run --scope default -- ./scripts/deploy.sh
si vault run --scope default -- bash -lc 'echo "$OPENAI_API_KEY" | wc -c'
```

Docker exec (env for that exec only):

```bash
si vault docker exec --scope default --container <name> -- ./app
```

## Identity

Vault encryption identity is Sun-managed.

```bash
si vault keygen           # ensure identity exists in Sun
si vault keygen --rotate  # rotate identity (dangerous for old ciphertext)
```

## Important Behavior Changes

- `si vault sync push` and `si vault sync pull` are intentionally unsupported in remote mode.
- `si sun vault backup push/pull` is also not used for normal vault flow.
- Trust commands are informational in Sun mode (`trust: n/a (sun-managed)`).
- `si vault fmt` is unsupported in Sun mode (no local dotenv formatting target).
- `si vault decrypt --in-place` is unsupported in Sun mode (no local plaintext materialization).
- `si vault recipients add/remove` is unsupported in Sun mode (single Sun-managed identity recipient).

## Environment Overrides

- `SI_SUN_BASE_URL`
- `SI_SUN_TOKEN`
- `SI_VAULT_SCOPE` (preferred)
- `SI_VAULT_FILE` (compat alias)

## Security Notes

- `--reveal` prints plaintext to stdout.
- Prefer `--stdin` for `set` to avoid shell history leaks.
- `si vault run` / `si vault docker exec` can block when plaintext values are present unless `--allow-plaintext` is explicitly set.
