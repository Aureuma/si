# `si vault` (SI Vault native format, Fort/Vault aligned)

`si vault` encrypts local `.env` files and manages per `repo/env` key material via local keyring state.

Design goals:
- dotenv-first workflow
- encrypted values committed to `safe`
- deterministic key names:
  - `SI_VAULT_PUBLIC_KEY` (stored in `.env` file)
  - private key material resolved from local SI vault keyring only (no env key material overrides)

Architecture boundary:
- SI Vault cryptography is local and file/keyring based.
- Fort is the only API wrapper for policy/auth over SI Vault operations.

## Fort Boundary (No Overlap Contract)

- SI Vault owns cryptography and `.env` ciphertext format only.
- Fort owns remote API authn/authz and policy enforcement only.
- SI Vault does not implement remote policy/auth decisions.
- Fort does not implement independent secret persistence or crypto key generation.
- Runtime agents should consume secrets through Fort; SI Vault CLI remains the local maintenance/admin tool.
- Inside SI runtime workers, local `si vault` secret commands are blocked by default and must use `si fort`.

## `si fort` Wrapper Contract

- `si fort` wraps the native `fort` binary and keeps runtime auth file-based.
- `si codex spawn ...` and `si codex shell ...` provision or reuse a profile-scoped Fort session under `~/.si/codex/profiles/<profile>/fort/`.
- Host bootstrap/admin auth for provisioning uses the bootstrap token files at `~/.si/fort/bootstrap/admin.token` and `~/.si/fort/bootstrap/admin.refresh.token`.
- Runtime worker sessions use profile-local file-backed token paths for the short-lived access token and rotating refresh token.
- Wrapper behavior:
  - prefers caller-supplied runtime token paths (`FORT_TOKEN_PATH`, `FORT_REFRESH_TOKEN_PATH`) and refreshes those file-backed sessions in place when possible
  - otherwise prefers the Fort session under `CODEX_HOME/fort/` when `CODEX_HOME` is set
  - otherwise prefers the active profile-scoped Fort session under `~/.si/codex/profiles/<profile>/fort/` and refreshes that file-backed session in place when possible
  - fails loudly for runtime secret commands (`get`, `set`, `list`, `batch-get`, `run`) when no usable runtime session exists or runtime refresh fails
  - uses bootstrap/admin auth only for explicit provisioning and admin commands (`agent ...`, `auth issue|login|list|revoke`, `auth session open`)
  - runtime refresh is owned by the profile-scoped Fort refresher
  - passes explicit token-file auth to native `fort` when default files are available (no bearer token argv injection)
  - rejects deprecated token-value env vars (`FORT_TOKEN`, `FORT_REFRESH_TOKEN`)
  - strips legacy token env entries from child process env if present
- Operational guidance:
  - keep `~/.si/fort/bootstrap/*` for break-glass recovery only
  - keep routine Fort access in `~/.si/codex/profiles/<profile>/fort/access.token` and `refresh.token`
  - Codex profile provisioning explicitly requests a `30d` refresh-session TTL; Fort's general default may be shorter for non-Codex sessions
- For flags that belong to native `fort` global options, pass through after `--`:
  - `si fort -- --host https://fort.aureuma.ai doctor`

## Core Model

- Secrets live in local `.env` files (encrypted values).
- `SI_VAULT_PUBLIC_KEY` is inserted at file top when missing.
- Encrypted values use prefix `encrypted:si-vault:`.
- Legacy `encrypted:` payloads are accepted for backward compatibility.
- Key material is scoped by `repo/env` and stored in local keyring file:
  - default: `~/.si/vault/si-vault-keyring.json`
  - override: `SI_VAULT_KEYRING_FILE`
- This SI Vault keyring is a local JSON state file, not the OS keychain/secret-service store.
- A single canonical keypair is enforced across all keyring scopes to prevent key sprawl.
- Legacy identity/private-key env variables are ignored with warnings.

## Quickstart

Generate or load keypair for current repo/env:

```bash
si vault keypair --env dev
```

Encrypt `.env`:

```bash
si vault encrypt --env-file .env --env dev
```

Decrypt to stdout:

```bash
si vault decrypt --env-file .env --env dev
```

Decrypt in place:

```bash
si vault decrypt --env-file .env --env dev --inplace
```

Restore last encrypted state:

```bash
si vault restore --env-file .env
```

Run commands with decrypted env at runtime:

```bash
si vault run --env-file .env --env dev -- cargo run --bin server
```

For SI runtime workers:
- Use `si fort ...` for secret access.
- Direct local `si vault` secret commands are blocked in worker sessions.

## Encryption Behavior

- `si vault encrypt` does not re-encrypt already-encrypted values by default.
- Use `--reencrypt` to rotate ciphertext.
- `--reencrypt` decrypts first, then encrypts plaintext again.

## Pre-commit Guard

Install hook:

```bash
si vault hooks install
```

The hook runs `si vault check --staged --all` and blocks commits if plaintext values are found in `.env*` files.
It resolves `si` in this order: `SI_BIN`, a repo-local `./si`, then `si` on `PATH`.

## Commands

- `si vault keypair` / `si vault keygen`
- `si vault status`
- `si vault check`
- `si vault hooks <install|status|uninstall>`
- `si vault encrypt`
- `si vault decrypt`
- `si vault restore`
- `si vault set`
- `si vault unset`
- `si vault get`
- `si vault list` / `si vault ls`
- `si vault run`
