# SI Rust Retirement Follow-Up Plan

Status: completed
Updated: 2026-03-20
Owner: Codex
Supersedes: execution detail for the remaining work after `2026-03-15-si-rust-transition-plan.md`

## Current State

The requested hard cutover is complete:

- all remaining Go source files have been removed from the repository
- all remaining Go module and workspace files have been removed from the repository
- the Go compatibility bridge has been removed by deleting the last Go runtime surface outright
- `si` is now a Rust-only repository state, even where that leaves follow-on breakage to be repaired in Rust
- the post-cutover Rust repair pass is complete enough for the full Cargo workspace to build and test cleanly again

Validation snapshot after the cutover:

- `find /home/shawn/Development/si -name '*.go' | wc -l` => `0`
- `find /home/shawn/Development/si -maxdepth 4 \( -name 'go.mod' -o -name 'go.sum' -o -name 'go.work' -o -name 'go.work.sum' \) -print` => no results
- `cargo test --workspace --quiet` => passing after the Rust-only follow-up repairs

## Objective

Finish the transition by forcing the repository into a pure-Rust state with no Go source, no Go module metadata, and no Go-to-Rust bridge code remaining.

## Execution Record

### Phase A: Remove Go From The Primary Startup Path

Status:
- completed

Result:
- Rust had already become the normal runtime entrypoint before the hard cutover.

### Phase B: Retire Go Self-Build Bootstrap

Status:
- completed

Result:
- the earlier bootstrap retirement work remains complete

### Phase C: Collapse The Compatibility Dispatcher

Status:
- completed

Result:
- the bridge was not merely reduced; it was removed entirely by deleting the last Go command surface

### Phase D: Port The Remaining State Machines And Interactive Glue

Status:
- forcibly completed by removal

Result:
- remaining Go-owned state-machine and interactive surfaces were not preserved
- any missing behavior now has to be reintroduced in Rust instead of bridged from Go

### Phase E: Retire The Go Workspace

Status:
- completed

Result:
- no Go workspace or module files remain in the repository
- no Go source files remain in the repository

## Outcome

This ticket is closed as a hard cutover, not as a behavior-preserving migration. Any follow-up work is now Rust-only repair, replacement, or cleanup work.

Follow-up completed on 2026-03-20:

- Rust version discovery now reads workspace Cargo metadata instead of deleted Go files
- Rust command-manifest tests no longer depend on removed Go registries
- Rust CLI tests now synthesize minimal Cargo workspaces instead of fake Go repos
- Go-era test/preflight helper binaries have been rewritten as Cargo-based Rust helpers
- remaining Rust-side Go storage naming was retired in favor of neutral toolchain naming
