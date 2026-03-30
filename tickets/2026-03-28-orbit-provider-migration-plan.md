# Orbit Provider Migration Plan

Date: 2026-03-28
Status: completed
Completed: 2026-03-30

## Completion note

This migration is complete.

Final state:

- `si orbit <provider> ...` is the public provider namespace
- `si orbit list` replaced the old `si providers characteristics` path
- removed public top-level provider roots now fail with replacement guidance
- stale public Orbitals residue was removed or rewritten in the active docs/help/skills surface
- provider help and representative auth/status flows remain covered by the Rust CLI test suite

Intentional leftovers:

- historical changelog entries that describe the old command surface remain as historical record

## Goal

Rebrand first-party provider bridges as `orbit` commands and move all provider-facing entrypoints under a single top-level command:

- `si orbit cloudflare ...`
- `si orbit apple ...`
- `si orbit aws ...`
- `si orbit gcp ...`
- `si orbit google ...`
- `si orbit openai ...`
- `si orbit oci ...`
- `si orbit stripe ...`
- `si orbit workos ...`
- `si orbit github ...`

At the same time:

- remove stale Orbitals residue that refers to the old catalog/install system
- fold the current `si providers characteristics` helper into the new orbit surface
- keep the implementation scalable so additional provider bridges can be added under `si orbit <provider>`

## Current State

Observed in `si`:

- top-level provider commands already exist for the provider bridges listed above
- `si providers` only exposes a single characteristics-style listing helper
- `si-command-manifest` still advertises a stale `orbits` root entry
- `si-tools` still contains `orbits-test-runner`
- docs and changelog still contain large amounts of old Orbitals language and command references

Observed in neighboring repos:

- `fort`, `surf`, and `viva` do not currently contain relevant orbit/orbits residue
- `_agentic/openclaw` contains its own extension/plugin/orbit terminology, but that is reference material, not migration scope

## Architectural Direction

### Command model

Use a single top-level `orbit` command in singular form as the stable umbrella for provider bridges.

Why:

- reduces top-level command sprawl
- gives a single scalable namespace for current and future providers
- keeps provider additions predictable for humans and coding agents

### Provider layout

Do not collapse provider crates together.

Keep provider crates as independent Rust crates, but make the CLI treat them as children of the `orbit` namespace. The command tree should move; the provider crates should remain independently owned implementation units.

### Compatibility posture

Preferred end state:

- `si orbit <provider> ...` is the supported path
- old top-level provider roots are removed from help and command dispatch

Because this is a very large surface, the implementation may use temporary hidden aliases internally only if required to keep test and migration risk manageable. The public CLI should present only `si orbit ...`.

## Target Command Shape

Root:

- `si orbit list`
- `si orbit cloudflare ...`
- `si orbit apple ...`
- `si orbit aws ...`
- `si orbit gcp ...`
- `si orbit google ...`
- `si orbit openai ...`
- `si orbit oci ...`
- `si orbit stripe ...`
- `si orbit workos ...`
- `si orbit github ...`

Provider list behavior:

- `si providers characteristics`
  becomes
- `si orbit list`

`si orbit list` should show the provider capability summary that is currently produced by `si providers characteristics`.

## Cleanup Scope

Remove stale Orbitals residue in `si` that is no longer part of the supported product surface:

- stale command-manifest root entries for `orbits`
- stale test runner binaries and shell wrappers for the old Orbitals system
- stale docs and command references for `si orbits ...`
- stale changelog/help references where they describe the old Orbitals catalog/install subsystem as active

Do not remove unrelated `_agentic/openclaw` references. They are external inspiration only.

Do not modify `fort`, `surf`, or `viva` unless actual migration residue is found there. Current scan shows none that match this work.

## Implementation Plan

### Phase 1: Introduce `si orbit`

1. Add a new top-level `Orbit` command enum in `si-cli`.
2. Add nested provider subcommands under `Orbit`.
3. Move provider dispatch from top-level command matching into `Orbit` dispatch.
4. Add `OrbitListCommand` that reuses the current provider characteristics implementation.

### Phase 2: Remove old top-level provider roots

1. Remove `Cloudflare`, `Apple`, `Aws`, `Gcp`, `Google`, `OpenAI`, `Oci`, `Stripe`, `WorkOS`, and `GitHub` from the public top-level command surface.
2. Remove `Providers` from the public top-level command surface.
3. If temporary hidden compatibility shims remain in code, ensure they are hidden from help and command manifests.
4. Update help summaries and command descriptions so `orbit` is the only public provider namespace.

### Phase 3: Remove stale Orbitals system residue

1. Remove stale `orbits` entry from `si-command-manifest`.
2. Remove `orbits-test-runner` binary and its wrappers if they are no longer exercised by the repo.
3. Remove or rewrite old Orbitals docs to reflect the new `orbit` provider namespace only.
4. Remove stale changelog lines only where they directly misrepresent the current surface.

### Phase 4: Keep the architecture scalable

1. Centralize provider registration metadata in one `OrbitProviderCommand` enum instead of scattering top-level roots.
2. Reuse existing provider subcommand enums rather than rewriting provider internals.
3. Keep `si orbit list` backed by the existing provider characteristics function so new providers only need one registry addition.

## Testing Plan

### CLI shape

Verify:

- `si --help` shows `orbit` and no longer shows moved provider roots
- `si orbit --help` lists all provider subcommands
- `si orbit list --help` is clear and stable
- `si orbit <provider> --help` preserves existing provider subcommand trees

### Command behavior

Verify representative commands still dispatch correctly after the move:

- `si orbit aws auth status`
- `si orbit github auth status`
- `si orbit openai auth status`
- `si orbit cloudflare auth status`
- `si orbit gcp auth status`

### Legacy removal

Verify old public roots are no longer advertised and cannot be discovered through help/manifests:

- `si providers`
- `si aws`
- `si github`
- `si openai`
- and the other moved provider roots

If temporary hidden compatibility roots remain during migration, keep them undocumented and treat them as internal-only shims.

### Docs/manifests

Verify:

- command reference uses `si orbit ...`
- root command manifests no longer advertise stale `orbits`
- no stale `si orbits` references remain unless explicitly retained in historical changelog context

### Regression discipline

Use focused CLI tests first, then run a smaller set of real command help smokes with the built binary.
If hidden compatibility shims remain, ensure the public `si orbit ...` path is what the tests and docs exercise.

## Design Notes

OpenClaw’s useful lesson is not its plugin runtime. The useful lesson is its single integration namespace and consistent per-integration layout. For `si`, the corresponding move is a single `orbit` namespace with first-party provider crates behind it.

This plan deliberately avoids adding a plugin system or gateway abstraction. The immediate win is command-surface coherence and clearer provider scaling, not a runtime extension architecture.

## Acceptance Criteria

- all listed first-party providers are publicly documented and discoverable through `si orbit <provider> ...`
- `si providers` is removed or fully replaced by `si orbit list`
- stale Orbitals residue inside `si` is removed or rewritten
- help, command manifests, and docs all align on `orbit`
- focused provider help and auth/status flows still work after the move
