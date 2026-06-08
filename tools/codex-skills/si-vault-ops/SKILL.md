---
name: si-vault-ops
description: Use this skill only for SI Vault maintenance and implementation debugging (`si vault ...`) including keypair/check/status/get/set/run operations; use `si fort` for operator secret workflows.
---

# SI Vault Ops

Use this workflow for SI Vault maintenance. For operator secret reads, writes,
runtime env injection, credentials, bootstrap flows, and repo-scoped secret
work, use `si fort` instead.

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
4. Run commands with decrypted env only for maintenance or implementation debugging:
```bash
si vault run -- <cmd>
```

## Guardrails

- Never print full secret values unless explicitly requested.
- Prefer `si fort run` for operator workflows.
- Use `si vault run` only when Fort is explicitly not the command boundary for
  maintenance or implementation debugging.
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
