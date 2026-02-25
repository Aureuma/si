# SI Vault Sun-Only Plan (CLI-First)

## Goals
- Sun API is the source of truth for `si vault` read/write flows.
- No local trust handshake requirements for normal Sun vault usage.
- No local private key file requirement for normal Sun vault usage.
- Simple UX for developers running scripts, Docker commands, and binaries.

## Constraints
- Keep CLI ergonomics simple and opinionated.
- Preserve compatibility flags where practical (`--file` aliasing to scope).
- Avoid hidden local secret persistence in default flow.

## Design

1. Backend mode
- Force effective vault backend to `sun`.
- Keep legacy mode values as accepted aliases that normalize to `sun`.

2. Scope model
- Replace path-centric vault targeting with a logical scope model in Sun mode.
- `vault.file` becomes the default logical scope (default: `default`).
- Prefer `--scope`; keep `--file` as a compatibility alias.

3. Identity model
- Use Sun-managed vault identity object for encryption/decryption key material.
- `si vault keygen` ensures/rotates Sun identity.
- No local key file required in normal operations.

4. Secret operations
- `set/get/list/unset/history` always use Sun KV object APIs.
- `run` and `docker exec` decrypt in-memory and inject env at runtime.

5. Safety and status
- `vault status` should fail non-zero when Sun auth/identity/KV is unavailable.
- Trust is reported as `n/a (sun-managed)` in Sun mode.

6. Legacy command boundaries
- Keep file-centric commands (`fmt/encrypt/decrypt/check/recipients`) available for compatibility, but remote-first commands do not depend on them.
- `vault sync push/pull` return explicit unsupported errors in remote mode.

## Critique Loop (what could go wrong)

1. Scope collisions
- Risk: path-like inputs normalize unexpectedly to the same scope.
- Mitigation: deterministic scope normalization, explicit `--scope`, and visible `scope:` output.

2. Silent fallback to local identity
- Risk: hidden local key material appears again and diverges from cloud identity.
- Mitigation: remove non-Sun identity fallback in strict Sun flow.

3. Legacy tests and docs drift
- Risk: local dotenv assumptions fail CI and mislead users.
- Mitigation: update tests to Sun contracts; skip obsolete local-only e2e tests; rewrite docs/help.

4. Runtime availability
- Risk: commands appear successful without Sun auth.
- Mitigation: strict client resolution and non-zero status failure on missing auth/identity.

## Best-practice references used
- HashiCorp Vault KV v2 + versioning model:
  https://developer.hashicorp.com/vault/docs/secrets/kv
- HashiCorp Vault production hardening guidance:
  https://developer.hashicorp.com/vault/docs/concepts/production-hardening
- Doppler CLI runtime injection pattern (`run`):
  https://docs.doppler.com/docs/accessing-secrets
- 1Password CLI + service-account/tokenized CLI flows:
  https://developer.1password.com/docs/cli

## Implementation checklist
- [x] Sun-only backend normalization in `si`.
- [x] Logical scope resolution for Sun mode.
- [x] Strict Sun identity sync for remote vault operations.
- [x] Remote-only behavior for `set/get/list/unset/run/docker exec/history/status/init/use`.
- [x] `vault status` non-zero on unavailable auth/identity/KV.
- [x] Targeted tests updated to Sun model.
- [x] Full `go test ./tools/si/...` pass.
- [x] `si build self` + `si build image` pass.
