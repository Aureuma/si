# Archived Plan: Sun-Remote Vault

Status: superseded.

This plan represented an older direction where Sun was considered part of SI Vault runtime secret flows.

Current architecture:
- Fort is the only policy/authentication API layer for SI Vault operations.
- SI Vault cryptography and key material handling remain in SI Vault + `safe`.
- Sun remains independent for profile/taskboard/machine/orbit workflows and is not in the SI Vault/Fort secret data path.

Use these documents instead:
- `docs/VAULT.md`
- `docs/SUN.md`
- `../fort/docs/GUIDING_PRINCIPLES.md` (Fort repository)
