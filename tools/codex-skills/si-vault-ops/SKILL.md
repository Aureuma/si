---
name: si-vault-ops
description: Use this skill when working with SI vault encryption and secure env workflows (`si vault ...`) including keypair/check/status/get/set/run operations.
---

# SI Vault Ops

Use this workflow for secure secret management with SI vault.

## Fast path

1. Check vault state first:
```bash
si vault status
si vault check
```
2. If needed, initialize:
```bash
si vault keypair
```
3. Read or update keys:
```bash
si vault get KEY
si vault set KEY value
si vault unset KEY
```
4. Run commands with decrypted env:
```bash
si vault run -- <cmd>
```

## Guardrails

- Never print full secret values unless explicitly requested.
- Prefer `si vault run` over exporting decrypted variables into shell history.
- Keep file paths explicit when not using defaults (`--file`).
- For local key issues, inspect `si vault status` and re-run `si vault keypair` before rotating.

## Validation

- After writes, run:
```bash
si vault check
si vault status
```
- Confirm expected key presence with:
```bash
si vault list
```
