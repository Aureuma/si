# `si vault` (dotenvx-style, Sun key-backed)

`si vault` now encrypts local `.env` files and keeps decryption keys in Sun by `repo/env`.

Design goals:
- dotenv-first workflow
- no private key files on disk
- deterministic key names:
  - `SI_VAULT_PUBLIC_KEY` (stored in `.env` file)
  - `SI_VAULT_PRIVATE_KEY` (fetched from Sun API only)

## Core Model

- Secrets live in local `.env` files (encrypted values).
- `SI_VAULT_PUBLIC_KEY` is always inserted at the beginning of the file.
- Encrypted values use prefix `encrypted:si-vault:`.
- Sun stores key material per `repo/env`:
  - `repo` inferred from current git repo directory name (or `--repo`)
  - `env` defaults to `dev` (or `--env`)

## Quickstart

Authenticate to Sun:

```bash
si sun auth login --url <sun-url> --token <token> --account <slug>
```

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
si vault run --env-file .env --env dev -- go run ./cmd/server
```

## Encryption Behavior

- `si vault encrypt` does not re-encrypt already-encrypted values by default.
- Use `--reencrypt` to rotate ciphertext.
- `--reencrypt` decrypts first, then encrypts plaintext again (prevents double-encryption corruption).

## Pre-commit Guard

Install hook:

```bash
si vault hooks install
```

The hook runs `si vault check --staged` and blocks commits if plaintext values are found in `.env*` files.

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
- `si vault docker exec`
