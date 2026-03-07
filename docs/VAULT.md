# `si vault` (dotenvx-style, Fort/Vault aligned)

`si vault` encrypts local `.env` files and manages per `repo/env` key material via local keyring state.

Design goals:
- dotenv-first workflow
- encrypted values committed to `safe`
- deterministic key names:
  - `SI_VAULT_PUBLIC_KEY` (stored in `.env` file)
  - `SI_VAULT_PRIVATE_KEY` (resolved from local keyring/env)

## Core Model

- Secrets live in local `.env` files (encrypted values).
- `SI_VAULT_PUBLIC_KEY` is inserted at file top when missing.
- Encrypted values use prefix `encrypted:si-vault:`.
- Key material is scoped by `repo/env` and stored in local keyring file:
  - default: `~/.si/vault/si-vault-keyring.json`
  - override: `SI_VAULT_KEYRING_FILE`

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
si vault run --env-file .env --env dev -- go run ./cmd/server
```

## Encryption Behavior

- `si vault encrypt` does not re-encrypt already-encrypted values by default.
- Use `--reencrypt` to rotate ciphertext.
- `--reencrypt` decrypts first, then encrypts plaintext again.

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
