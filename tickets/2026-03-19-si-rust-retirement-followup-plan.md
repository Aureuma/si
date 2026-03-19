# SI Rust Retirement Follow-Up Plan

Status: in_progress
Updated: 2026-03-19
Owner: Codex
Supersedes: execution detail for the remaining work after `2026-03-15-si-rust-transition-plan.md`

## Current State

`si` is already Rust-primary in important ways, but it is not Rust-retired:

- repo root Cargo workspace exists and currently contains 25 Rust crates
- release/docs/build paths already point at `si-rs` as the primary binary
- the old Go top-level launcher is retired; `si-rs` is the runtime entrypoint
- the hidden Go toolchain bootstrap path has been removed; remaining Go helpers now require an already-installed local `go`
- `tools/si/rust_cli_bridge.go` still contains a large compatibility dispatcher that routes many command families between Go and Rust
- the repo-level `go.work` workspace has been retired; the remaining Go code now runs as the single module at `tools/si/go.mod`
- `tools/si` still contains 413 `.go` files

This means the repo is past “Rust introduced” and into “Go retirement”. The remaining work is not about proving Rust is viable. It is about removing the last places where Go is still operationally required.

## Objective

Finish the `si` transition from Go to Rust by retiring the remaining Go runtime, build, and compatibility ownership without breaking the shipping CLI.

## Hard Requirements

- `si-rs` remains the source of truth for release artifacts and local builds.
- No new feature work lands in Go unless it is an explicit migration unblocker or production hotfix.
- Every Go-owned path that remains must have an owner, a retirement phase, and an exit test.
- Rust/Go behavior must not silently diverge during the burn-down.
- `go.work` and `tools/si/go.mod` are retirement targets, not permanent architecture.

## Remaining Go-Owned Surfaces

### 1. Entry and bootstrap ownership

- `tools/si/go.mod`

This file still keeps Go in the critical path for residual helper commands and module-local validation.

### 2. Compatibility dispatch ownership

- `tools/si/rust_cli_bridge.go`

This is now the largest single sign that the cutover is incomplete. It still owns:

- command-family gating (`shouldUseRust*CLI`)
- broad `maybeDispatchRustCLICompat(...)` routing
- many provider command shims
- codex/dyad/warmup/fort delegation seams

### 3. Legacy command/runtime ownership

The following families still materially exist under Go source even where Rust is already the preferred path:

- codex
- dyad
- fort runtime glue
- vault flows
- provider command families
- release/build helper glue
- settings/runtime helper code still duplicated across Go and Rust

### 4. Legacy test ownership

Most regression coverage still lives in Go tests under `tools/si`, which is useful now but blocks full Go retirement later. The final state needs Rust-native parity/integration coverage for the command families that still depend on Go tests as the source of truth.

## Migration Strategy

### Phase A: Remove Go From The Primary Startup Path

Goal:
- make Rust the only normal execution path for `si`

Implementation:
- remove the Go top-level launcher from the default shipped/runtime path
- remove any remaining release/install flows that expect in-process Go dispatch as a safety net
- document `si-rs` as the only supported local/runtime entrypoint

Validation:
- `cargo build --release --locked --bin si-rs`
- release/install smoke still works with no implicit Go fallback
- manual `si-rs help`, `si-rs version`, and one command from each bridged family

Exit criteria:
- Go is no longer a top-level runtime entrypoint for `si`

Progress:
- completed: the Go top-level launcher has been retired; `si-rs` is now the only supported runtime entrypoint

### Phase B: Retire Go Self-Build Bootstrap

Goal:
- remove Go toolchain bootstrap from the self-build story

Implementation:
- delete or retire `tools/si/self_go_bootstrap.go`
- ensure `si-rs build self` and release/install helpers are fully Rust-owned
- remove any docs/runbooks that still imply a Go bootstrap requirement

Validation:
- Rust self-build/install commands work on a clean machine with Rust toolchain only
- release-preflight path succeeds without using `go`

Exit criteria:
- local and release self-build paths do not depend on a Go toolchain

Progress:
- completed: the hidden Go download/bootstrap path has been removed from the remaining Go compatibility commands; `tools/si/self_go_bootstrap.go` is retired and the residual Go helper surface now only resolves an already-installed local/sibling `go` binary with a clear error instead of mutating the host toolchain state

### Phase C: Collapse The Compatibility Dispatcher

Goal:
- move command-family dispatch ownership out of Go bridge code and into Rust

Implementation:
- inventory all `maybeDispatchRustCLICompat(...)` call sites and group them by family
- for each family, move the command surface fully into Rust and delete the corresponding Go bridge branch
- reduce `shouldUseRust*CLI` gates until they disappear rather than remain as permanent toggles
- treat bridge deletion as the actual milestone, not just “Rust path exists”

Suggested retirement order:
1. `fort`, `warmup`, and read-mostly admin/diagnostic surfaces
2. provider families already mostly modeled in Rust crates
3. vault command family
4. codex and dyad long tail

Validation:
- targeted parity tests per family
- command snapshots for help/stdout/exit code
- one integration smoke per retired family

Exit criteria:
- `rust_cli_bridge.go` is reduced to a minimal temporary shim or removed entirely

Progress:
- in progress: the legacy `SI_EXPERIMENTAL_RUST_CLI` gate and repo-local `si-rs` artifact fallback have been removed from the bridge path
- in progress: stale Go-era compatibility tests for retired Rust command families (`orbits`, `publish`, `social`, legacy vault CRUD aliases, and removed public-probe assumptions) have been retired so `go test ./tools/si` validates the current Rust CLI contract instead of deleted surfaces
- validated: `go test ./tools/si -count=1 -timeout 10m` passes against the current Rust-primary CLI surface
- completed: `go.work` has been removed and the remaining Go test/docs/CI helpers now target `tools/si` as a normal single-module Go package instead of a repo-level workspace
- completed: the `warmup` family no longer uses optional Go fallback at runtime; `shouldUseRustWarmupCLI` is now hard-wired on and the remaining tests validate the Rust-owned autostart/state/marker flow
- completed: the Rust-only root command entrypoints now correctly unpack `(delegated, err)` before enforcing `requireRustCLIDelegation(...)`, repairing the broken Go compatibility module build after the Rust-primary cutover
- completed: optional Go helper paths that still exist during retirement now short-circuit cleanly when `si-rs` is unavailable instead of hard-failing metadata-only and state-file operations, so the remaining `tools/si` module validation still passes while real Rust-owned runtime entrypoints continue to require the Rust binary
- completed: the fully Rust-owned root command handlers have had their unreachable in-file Go fallback dispatch bodies removed (`apple`, `aws`, `cloudflare`, `dyad`, `gcp`, `github`, `google`, `oci`, `openai`, `stripe`, `vault`, `workos`, and related wrappers), reducing the visible Go ownership surface without changing the active Rust delegation contract
- completed: the Rust-owned `providers characteristics` and `providers health` entrypoints now have their dead in-file Go implementations removed, matching the existing `requireRustCLIDelegation(...)` enforcement and shrinking another already-modeled provider surface
- completed: dead nested GitHub auth/context/doctor Go command handlers and their stale direct-invocation tests have been removed, aligning the remaining Go module with the fact that `si github ...` is already Rust-required at the root entrypoint
- completed: the entirely dead Go command files for `github repo`, `github pr`, and `github issue` have been removed, since those families are already dispatched through the Rust-owned root GitHub entrypoint and no longer have live Go call sites
- completed: the remaining dead Go command layer for `github branch`, `github release`, and `github workflow` has been removed while preserving the small helper functions still covered by Go tests, cutting most of the last nested GitHub runtime ownership out of `tools/si`
- completed: the dead Go command surfaces for `github raw`, `github graphql`, and `github secret` have been retired, leaving only the shared client/query helpers and the small secret-encryption helpers that still have direct Go test coverage

### Phase D: Port The Remaining State Machines And Interactive Glue

Goal:
- move the last correctness-sensitive runtime logic out of Go

Implementation:
- migrate residual session/runtime flows for:
  - codex
  - dyad
  - fort profile/runtime integration
  - vault secure-env execution
- move interactive/TTY/tmux ownership to Rust crates or Rust tools binaries
- remove duplicated path/settings/runtime logic still living in Go

Validation:
- multi-profile spawn/respawn matrix
- dyad spawn/status/peek/logs/exec matrix
- fort wrapper/session refresh matrix
- vault run/get/set/list parity matrix

Exit criteria:
- no correctness-sensitive runtime state machine is Go-owned

### Phase E: Retire The Go Workspace

Goal:
- make Go non-required for normal development and release

Implementation:
- remove `go.work`
- delete `tools/si/go.mod` after the last Go code path is gone
- delete obsolete Go files from `tools/si`
- preserve only intentionally external compatibility artifacts, if any, and document them explicitly

Validation:
- `cargo fmt --check`
- `cargo clippy --workspace --all-targets -- -D warnings`
- `cargo test --workspace`
- `cargo build --release --locked --bin si-rs`
- release artifact generation from Rust only

Exit criteria:
- `si` no longer needs Go for build, test, release, or runtime

Progress:
- completed: `go.work` has been removed, the Rust-owned test runners now resolve the repo through `tools/si/go.mod`, and module-local Go validation works through both `cd tools/si && go test ./...` and the repo-root `./tools/test.sh` wrapper

## Immediate Next Tickets

### Ticket 1: Startup And Self-Build De-Go

Scope:
- `tools/si/self_go_bootstrap.go`
- release/install docs and tests that still assume Go fallback/bootstrap

Deliverable:
- Rust-only normal startup and Rust-only self-build path

### Ticket 2: Bridge Burn-Down By Family

Scope:
- `tools/si/rust_cli_bridge.go`

Deliverable:
- remove bridge branches family-by-family, starting with `fort`, `warmup`, and already-modeled providers

### Ticket 3: Rust-Native Regression Matrix

Scope:
- convert critical Go parity/integration tests into Rust-owned integration suites where the Rust path is already primary

Deliverable:
- Go tests stop being the only source of truth for Rust-primary behavior

### Ticket 4: Go Workspace Retirement

Scope:
- `tools/si/go.mod`
- residual Go files after bridge/state-machine retirement

Deliverable:
- Go removed from active workspace and normal release path

## Validation Gates

### Required During Transition

- `cargo fmt --check`
- `cargo clippy --workspace --all-targets -- -D warnings`
- `cargo test --workspace`
- `cargo build --release --locked --bin si-rs`
- targeted compatibility checks for any remaining Go bridge family touched

### Required Before Go Retirement

- all user-facing docs reference Rust-primary commands only
- release artifacts are produced from Rust only
- no runtime image depends on `si-go`
- no top-level entrypoint depends on Go fallback
- no active CI lane requires `go.work` for normal `si` validation

## Definition Of Done

- `si-rs` is the only supported normal runtime entrypoint
- `si-rs build self` and release/install paths are Rust-only
- `rust_cli_bridge.go` is removed or reduced to a temporary non-runtime shim with a dated deletion ticket
- `tools/si/self_go_bootstrap.go` is gone and there is no Go top-level runtime entrypoint
- `go.work` and `tools/si/go.mod` are gone
- remaining Go files, if any, are explicitly justified compatibility artifacts rather than active CLI ownership
