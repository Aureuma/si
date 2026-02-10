# `si vault` (Git-Based Encrypted Credentials)

`si vault` manages credentials in `.env.<env>` files with values encrypted inline using age recipients, designed to be committed to a separate private git repo (usually as a submodule).

Goals:
- encrypted at rest (git-friendly, PR-reviewable)
- minimal git noise (encrypted values are not rewritten unless you explicitly re-encrypt)
- Docker-friendly runtime injection (decrypt on host, inject into `docker exec` env for that exec only)
- local-only audit trail (no secret values logged)

## Model (Recommended Default)

Host repo:
- contains code
- includes a `vault/` git submodule pointing at a private vault repo

Vault repo (submodule checkout):
- contains encrypted env files:
  - `vault/.env.dev`
  - `vault/.env.prod`
  - etc

## Quickstart

From your host repo (a normal git repo):

1. Add/initialize the vault submodule and bootstrap the env file:
```bash
si vault init --submodule-url <git-url-for-private-vault-repo> --env dev
```

2. Set a secret (prefer `--stdin` to avoid shell history):
```bash
printf '%s' 'sk_test_...' | si vault set STRIPE_API_KEY --stdin --section stripe
```

3. Format the file to the canonical style (optional but recommended):
```bash
si vault fmt --env dev
```

4. Run a local process with secrets injected:
```bash
si vault run --env dev -- ./your-command --args
```

5. Inject secrets into an existing container process (`docker exec` env injection for that exec only):
```bash
si vault docker exec --container <name-or-id> --env dev -- ./your-command --args
```

## Dyads + Codex Containers

Dyad and Codex containers are built from the same unified image (`aureuma/si:local`) which includes `/usr/local/bin/si`.
That means you can run read-only vault commands (like `si vault status`) from inside a dyad container via `si dyad exec`.

For secret injection, prefer running from the host:

```bash
# Inject decrypted env for that exec only (decrypt happens on the host).
si vault docker exec --container si-actor-<dyad> --env dev -- ./your-command --args
```

## Prevent Plaintext Commits (Git Hooks)

`si vault init` installs a best-effort local `pre-commit` hook inside the vault repo to block committing dotenv files that contain plaintext values.

You can also manage hooks explicitly:
```bash
si vault hooks install --vault-dir vault
si vault hooks status --vault-dir vault
si vault hooks uninstall --vault-dir vault
```

The hook runs `si vault check --staged --all --vault-dir .` inside the vault repo.

Notes:
- Git hooks are local-only and can be bypassed with `git commit --no-verify`.
- For stronger enforcement, add a CI check in the vault repo that fails if any `.env*` file contains plaintext values.

## Multi-Environment Files

`--env <name>` maps to `.env.<name>` inside the vault dir, for example:
- `--env dev` -> `vault/.env.dev`
- `--env prod` -> `vault/.env.prod`

## Formatting

`si vault fmt` enforces a single canonical `.env` style:
- header block:
  - `# si-vault:v1`
  - one or more `# si-vault:recipient age1...` lines
  - one blank line after header
- sections:
  - divider: `# ------------------------------------------------------------------------------`
  - header: `# [stripe]`, `# [workos]`, etc
- keys:
  - `KEY=value` (no spaces around `=`)

Mutating commands support `--format` to run `fmt` after the minimal edit.

## Trust Model (TOFU)

`si vault` uses trust-on-first-use (similar to `ssh known_hosts`) to prevent silent recipient drift:
- local trust store: `~/.si/vault/trust.json`
- keyed by `(host repo root, vault dir, env)`
- stores:
  - vault repo URL (when available)
  - recipients fingerprint

Commands:
- `si vault trust status` shows stored vs current fingerprint.
- `si vault trust accept` records the current fingerprint.
- `si vault trust forget` removes the trust entry.

Most mutating/decrypting commands require trust to be established.

## Key Storage

Device identities are age X25519 private keys. Resolution order:
1. `SI_VAULT_IDENTITY` (or `SI_VAULT_PRIVATE_KEY`) (CI/ephemeral)
2. `SI_VAULT_IDENTITY_FILE`
3. OS keyring/keychain (when available)
4. file backend (`vault.key_backend = "file"`, `vault.key_file = "~/.si/vault/keys/age.key"`)

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
