# SI Rust Follow-Up Record

Status: completed
Updated: 2026-03-20
Owner: Codex
Supersedes: execution detail for the remaining work after `2026-03-15-si-rust-transition-plan.md`

## Current State

The requested hard cutover is complete:

- `si` is now a Rust-only repository state, even where that leaves follow-on breakage to be repaired in Rust
- the post-cutover Rust repair pass is complete enough for the full Cargo workspace to build and test cleanly again

Validation snapshot after the cutover:

- `cargo test --workspace --quiet` => passing after the Rust-only follow-up repairs

## Objective

Finish the transition by forcing the repository into a pure-Rust state.

## Execution Record

### Phase A: Remove The Legacy Primary Startup Path

Status:
- completed

Result:
- Rust had already become the normal runtime entrypoint before the hard cutover.

### Phase B: Retire The Legacy Self-Build Bootstrap

Status:
- completed

Result:
- the earlier bootstrap retirement work remains complete

### Phase C: Collapse The Compatibility Dispatcher

Status:
- completed

Result:
- the legacy compatibility dispatcher has been retired

### Phase D: Port The Remaining State Machines And Interactive Glue

Status:
- forcibly completed by removal

Result:
- missing behavior had to be reintroduced directly in Rust after the cutover

### Phase E: Retire The Legacy Workspace

Status:
- completed

Result:
- the repository contains only the Rust workspace and supporting assets

## Outcome

This ticket is closed as a hard cutover and the requested Rust-only repository state has been completed.

Remaining migration work in the repos present in this workspace:

- none

Any future work should be treated as ordinary Rust feature work, maintenance, or cleanup.

Follow-up completed on 2026-03-20:

- Rust version discovery now reads workspace Cargo metadata
- Rust command-manifest tests no longer depend on legacy registries
- Rust CLI tests now synthesize minimal Cargo workspaces
- test/preflight helper binaries have been rewritten as Rust helpers
- legacy storage naming has been retired in favor of neutral toolchain naming
- release workflows, installer smoke workflows, runtime Dockerfiles, and operator docs have been updated to Rust-only assumptions
