# `si vault` (Encrypted Dotenv Credentials)

`si vault` manages credentials in dotenv files with values encrypted inline using age recipients. Vault commands operate on a single default env file (`vault.file` in settings) unless `--file` is provided.

Goals:
- encrypted at rest (git-friendly, PR-reviewable)
- minimal git noise (encrypted values are not rewritten unless you explicitly re-encrypt)
- Docker-friendly runtime injection (decrypt on host, inject into `docker exec` env for that exec only)
- local-only audit trail (no secret values logged)

## Model (Recommended Default)

- One encrypted dotenv file on disk (default: `~/.si/vault/.env`).
- One Sun backup snapshot object (`vault_backup/<name>`) for disaster-recovery pull/push.
- One Sun per-key object collection (`vault_kv.<scope>/<KEY>`) for direct cloud key reads/writes and revision history.
- Use `--file` to operate on a different file.
- Vault backend is Sun-only. Use `si vault backend use --mode sun` to pin settings explicitly.
- `sun`: Sun-backed mode (cloud hydrate + required cloud backup on mutating vault commands).

## Quickstart

From your host repo (or any local workspace):

0. Ensure you have a device identity (this stores a private age identity in your OS secure store by default):
```bash
si vault keygen
```

If you run `si sun auth login`, SI automatically sets `vault.sync_backend = "sun"` for that machine.

1. Bootstrap the default env file:
```bash
si vault init
```

To bootstrap a specific file path without changing your default file, pass `--file`:
```bash
si vault init --file /path/to/.env
```

To explicitly change the default vault file for future commands:
```bash
si vault use --file /path/to/.env
# or during init:
si vault init --file /path/to/.env --set-default
```

2. Set a secret (prefer `--stdin` to avoid shell history):
```bash
printf '%s' 'sk_test_...' | si vault set STRIPE_API_KEY --stdin --section stripe
```

3. Format the file to the canonical style (optional but recommended):
```bash
si vault fmt
```

4. Run a local process with secrets injected:
```bash
si vault run -- ./your-command --args
```

To run shell syntax (pipes, redirection, `&&`, etc), you can run a shell explicitly:
```bash
si vault run -- bash -lc 'echo "$STRIPE_API_KEY" | head -c 4'
```

Or use the built-in shell mode (note: this does not inherit functions/aliases from your current shell process; source them explicitly):
```bash
si vault run --shell -- 'source ./settings.sh; vps'
```

5. Inject secrets into an existing container process (`docker exec` env injection for that exec only):
```bash
si vault docker exec --container <name-or-id> -- ./your-command --args
```

6. Inspect cloud key history for a specific key:
```bash
si vault history STRIPE_API_KEY --limit 20
```

## Dyads + Codex Containers

Dyad and Codex containers are built from the same unified image (`aureuma/si:local`) which includes `/usr/local/bin/si`.
That means you can run read-only vault commands (like `si vault status`) from inside a dyad container via `si dyad exec`.

For secret injection, prefer running from the host:

```bash
# Inject decrypted env for that exec only (decrypt happens on the host).
si vault docker exec --container si-actor-<dyad> -- ./your-command --args
```

## Prevent Plaintext Commits (Git Hooks)

`si vault hooks install` installs a best-effort local `pre-commit` hook in the current git repo to block committing dotenv files that contain plaintext values.

You can also manage hooks explicitly:
```bash
si vault hooks install
si vault hooks status
si vault hooks uninstall
```

The hook runs `si vault check --staged --all`.

Notes:
- Git hooks are local-only and can be bypassed with `git commit --no-verify`.
- For stronger enforcement, add a CI check in the vault repo that fails if any `.env*` file contains plaintext values.

## Multiple Dotenv Files

By default, vault commands operate on the configured `vault.file`. To operate on a different dotenv file, pass `--file` explicitly:

```bash
si vault encrypt --file vault/.env.prod --format
si vault run --file vault/.env.prod -- ./your-command --args
si vault init --file /path/to/any/.env
```

Cross-repo guardrail:
- Use `--file` or run `si vault use --file <path>` when operating across repos/workspaces.

## Formatting

`si vault fmt` enforces a canonical header and key/value style:
- header block:
  - `# si-vault:v2`
  - one or more `# si-vault:recipient age1...` lines
  - one blank line after header
  - version is shared with the current encrypted value prefix (`encrypted:si:v2:`)
- sections:
  - section headers like `# [stripe]` are preserved as-authored (not lowercased/rewritten)
  - divider comment lines (e.g. `# ---------...`) are preserved as-authored
- keys:
  - `KEY=value` (no spaces around `=`)

Mutating commands support `--format` to run `fmt` after the minimal edit.

## Sun Vault Sync

You can sync the vault directly from the `si vault` namespace:

```bash
si vault sync status [--file <path>] [--name <name>] [--json]
si vault sync push [--file <path>] [--name <name>] [--allow-plaintext]
si vault sync pull [--file <path>] [--name <name>]
```

These commands map to the same Sun backup object flow used by `si sun vault backup ...`.
`push` also mirrors individual keys into Sun KV objects so `si vault get/list/run` can read directly from cloud-backed key state.
In `sun` backend mode, automatic backup hooks also run after recipient changes (`si vault recipients add/remove`).
In `sun` mode, vault commands also hydrate the local vault file from the latest Sun object before command execution.

You can inspect revision history for a key (set/unset metadata and timestamps) with:

```bash
si vault history <KEY> [--file <path>] [--limit <n>] [--json]
```

## Decrypting To Plaintext

By default, `si vault decrypt` decrypts values in-place in the same file on disk (similar to `dotenvx decrypt`).

This is intentionally dangerous: it writes plaintext secrets to disk. Prefer runtime injection (`si vault run`)
when possible, and re-encrypt immediately after editing.

To preview the decrypted file without modifying it:

```bash
si vault decrypt --file vault/.env --stdout
```

## Trust Model (TOFU)

`si vault` uses trust-on-first-use (similar to `ssh known_hosts`) to prevent silent recipient drift:
- local trust store: `~/.si/vault/trust.json`
- keyed by `(host repo root, env file)`
- stores the recipients fingerprint

Commands:
- `si vault trust status` shows stored vs current fingerprint.
- `si vault trust accept` records the current fingerprint.
- `si vault trust forget` removes the trust entry.

Most mutating/decrypting commands require trust to be established.

## Backend Selection

Inspect effective backend:

```bash
si vault backend status
```

Set backend mode:

```bash
si vault backend use --mode sun
```

Environment override:
- `SI_VAULT_SYNC_BACKEND=sun`

## Key Storage

Device identities are age X25519 private keys. Resolution order:
1. `SI_VAULT_IDENTITY` (or `SI_VAULT_PRIVATE_KEY`) (CI/ephemeral)
2. `SI_VAULT_IDENTITY_FILE`
3. Sun identity object (`vault_identity/<sun.vault_backup>`) when vault backend is `sun` and Sun auth is configured
4. OS secure store (Keychain on macOS, Secret Service on Linux) (`vault.key_backend = "keyring"` or `"keychain"`)
5. file backend (`vault.key_backend = "file"`, `vault.key_file = "~/.si/vault/keys/age.key"`)

To generate a new identity:
```bash
si vault keygen
```

To intentionally rotate (replace) an existing identity:
```bash
si vault keygen --rotate
```

Settings are configured in `~/.si/settings.toml` under `[vault]`.

Key file security:
- when using file backend, `si` requires the key file to be `0600` and not a symlink
- override (not recommended): `SI_VAULT_ALLOW_INSECURE_KEY_FILE=1`
- vault dotenv writes also refuse symlink targets by default
- override (not recommended): `SI_VAULT_ALLOW_SYMLINK_ENV_FILE=1`

## Audit Log

Default audit log:
- `~/.si/logs/vault.log` (JSONL)

Events include:
- `init`, `set`, `unset`, `encrypt`, `reveal`, `run`, `docker_exec`

Audit logs never include secret values.

## Security Notes (MVP)

- `--reveal` prints a secret to stdout. Use sparingly.
- Prefer `--stdin` for `set` to avoid shell history.
- `si vault run` and `si vault docker exec` refuse to proceed if plaintext keys exist unless you pass `--allow-plaintext`.
- dotenv keys are validated for safe env export (no whitespace, `=`, or control characters).
- `docker exec` env injection is per-exec; values are still transmitted to the Docker daemon. Treat remote Docker as highly privileged.
- `si vault docker exec` refuses insecure `DOCKER_HOST` by default; override with `--allow-insecure-docker-host` only if you understand the risk.

## Industry References

The vault + Sun backend model in SI aligns with common guidance from major secret-management systems:
- HashiCorp Vault production hardening guidance (TLS, auth boundaries, audit coverage):
  https://developer.hashicorp.com/vault/docs/concepts/production-hardening
- HashiCorp Vault KV concepts (versioned key-value workflow and metadata-first operations):
  https://developer.hashicorp.com/vault/docs/secrets/kv
- dotenvx Ops model (simple encrypted env ops, recovery, and team sync):
  https://dotenvx.com/docs/ops
- AWS Secrets Manager best practices (least privilege, rotation, monitoring):
  https://docs.aws.amazon.com/secretsmanager/latest/userguide/best-practices.html
- Google Cloud Secret Manager best practices (least privilege, rotation, replication/access controls):
  https://cloud.google.com/secret-manager/docs/best-practices
