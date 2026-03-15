# SI Rust Transition Plan

Status: in_progress
Updated: 2026-03-15
Owner: Codex

## Goal

Migrate `si` from the current large Go CLI into a modular Rust workspace without a flag day rewrite, while keeping the shipping Go CLI usable and releasable during the transition.

## Non-Negotiable Constraints

- `si` must remain releasable from `main` throughout the migration.
- Every new Rust slice must ship with build, test, and rollback criteria before it becomes the source of truth.
- High-risk runtime flows (`spawn`, `respawn`, `dyad`, `fort`, `vault`, provider auth) do not move first.
- New Rust code lives in the same repo and is introduced behind explicit compatibility boundaries.
- No silent behavioral drift: every migration phase needs parity tests or golden comparisons against the current Go behavior.

## Why This Architecture

The current Go implementation is effective but structurally expensive to evolve:

- `tools/si` is a very large single package with many multi-thousand-line files.
- command dispatch, settings, auth/session state, Docker orchestration, and provider surfaces are tightly interwoven.
- correctness-sensitive areas such as path resolution, session lifecycle, and provider contracts would benefit from stronger type boundaries and smaller ownership domains.

Rust is not being introduced for novelty. It is being introduced to force subsystem boundaries, typed state modeling, and safer long-lived runtime components.

## Target Rust Architecture

Workspace root: repo root `Cargo.toml`

Initial crate map:

- `rust/crates/si-core`
  Shared versioning, repo metadata, error helpers, and cross-cutting types.
- `rust/crates/si-config`
  Settings schema, path expansion, file loading, validation, and eventually environment/profile resolution.
- `rust/crates/si-cli`
  New Rust entrypoint and low-risk command families.

Planned follow-on crates:

- `rust/crates/si-process`
  Process execution, streaming IO, exit policy, retries, command tracing.
- `rust/crates/si-docker`
  Docker CLI/API wrappers, container/network/image operations, typed errors.
- `rust/crates/si-runtime`
  Runtime lifecycle orchestration shared by codex/dyad.
- `rust/crates/si-fort`
  Fort session state machine, token files, runtime refresher ownership.
- `rust/crates/si-vault`
  Vault encryption/decryption flows, trust metadata, secure env injection.
- `rust/crates/si-codex`
  `spawn`/`respawn`/`run`/`remove` lifecycle.
- `rust/crates/si-dyad`
  Dyad lifecycle and actor/critic coordination.
- `rust/crates/si-provider-*`
  Provider-specific bridges grouped by domain.
- `rust/crates/si-release`
  Release, publish, packaging, and installer workflows.

## Command Migration Shape

### Stage 1

- Rust owns low-risk read-only commands and shared libraries.
- Go remains the shipping entrypoint for all existing user workflows.

### Stage 2

- Go dispatches selected command families to Rust binaries or Rust-backed adapters.
- Parity tests compare command outputs and exit behavior.

### Stage 3

- Rust becomes the primary `si` binary.
- Go remains only as compatibility shims for flows not yet ported.

### Stage 4

- Remove Go implementations after parity, soak time, and release validation.

## Migration Phases

| Phase | Status | Outcome | Implementation focus | Required validation |
| --- | --- | --- | --- | --- |
| 0. Repo preparation | completed | `tickets/` reset and migration plan established | clear old tickets, define architecture, define parity rules | plan reviewed, `tickets/` contains only the active transition plan |
| 1. Rust workspace bootstrap | completed | Rust workspace builds/tests in repo without changing current behavior | workspace files, first crates, CI lane, version/path foundations | `cargo fmt`, `cargo clippy`, `cargo test`, existing Go build/tests still pass |
| 2. Shared config/runtime foundations | in_progress | Rust becomes source for settings/path/process primitives | settings loader, path expansion, command manifest, process abstraction | golden tests against Go settings/path behavior, cross-platform path tests |
| 3. Read-only command migration | in_progress | safe informational surfaces start moving to Rust | `version`, `help`, `providers health`, config inspection, diagnostics | CLI snapshots, golden stdout/exit code parity, smoke tests in CI |
| 4. Runtime substrate migration | in_progress | Docker/process/runtime primitives move under Rust ownership | process runner, Docker wrappers, network/image abstractions | integration tests with Docker, error-path tests, log/stream tests |
| 5. Security/runtime migration | in_progress | Fort/vault/session lifecycle moves to Rust with explicit state machines | Fort runtime agent, token state, locks, vault file handling | Fort integration matrix, concurrent refresh tests, teardown tests |
| 6. Codex/dyad lifecycle migration | in_progress | core container lifecycle ports to Rust | spawn/respawn/status/run/remove, tmux/dyad orchestration | container lifecycle matrix, regression parity suite, multi-profile smoke tests |
| 7. Provider migration | planned | provider families port incrementally | GitHub first, then low-complexity providers, then high-complexity providers | API contract tests, auth tests, fixture-based command parity |
| 8. Release/install migration | planned | release stack becomes Rust-native | packaging, install, npm/homebrew integration, release helpers | runbook dry run, installer smoke, release-preflight artifact checks |
| 9. Primary binary cutover | planned | Rust binary becomes default `si` | Go compatibility shell, packaging switch, release notes, rollback plan | full CI green, release candidate soak, Homebrew/npm/manual install verification |
| 10. Go retirement | planned | remove obsolete Go code paths | delete migrated Go modules and scripts, simplify repo | no runtime references left, docs updated, release published from Rust path |

## Detailed Work Items

### Phase 1: Rust workspace bootstrap

Status: completed

Implementation:

- create a workspace at repo root with committed toolchain/config files.
- add `si-core`, `si-config`, and `si-cli`.
- keep the first Rust slice intentionally read-only:
  - repo version reading
  - `.si` path/default resolution
  - settings subset parsing for `[paths]`
- add CI coverage for Rust formatting, linting, and tests.

Testing:

- `cargo fmt --check`
- `cargo clippy --workspace --all-targets -- -D warnings`
- `cargo test --workspace`
- `go build ./tools/si`
- targeted Go tests for version/path logic when touched

Exit criteria:

- Rust workspace is green locally.
- Rust workspace has a GitHub Actions lane.
- No current `si` user-facing behavior changes.

### Phase 2: Shared config/runtime foundations

Status: in_progress

Implementation:

- port settings schema progressively into `si-config`.
- add a typed command manifest so top-level command registration is data-driven instead of hand-wired.
- add `si-process` with consistent command execution, IO capture, timeout, and tracing behavior.
- add `si-docker` with typed operations for containers, images, networks, and bind mounts.

Testing:

- fixture-based settings parsing tests against real `settings.toml` variants from repo tests.
- snapshot tests for command manifest/help output.
- Docker abstraction unit tests plus opt-in integration tests.
- parity checks against `runtime_paths.go` behavior.

Exit criteria:

- path/settings/process foundations are callable from Rust without shell glue.
- command metadata is no longer encoded only inside Go switch/handler setup.

Progress notes:

- completed: initial Rust command manifest crate with parity tests against Go root command registration
- completed: Rust read-only `help` and `commands list` surface backed by the manifest
- completed: core settings subset for `schema_version`, `paths`, `codex`, and `dyad`
- completed: Rust read-only `settings show` surface backed by the config crate
- completed: initial `si-process` crate for typed command specs, env/cwd overrides, capture modes, and timeout handling
- completed: modular Rust settings loading for the first non-core modules (`surf` and `viva`) with parity tests ported from Go settings cases
- completed: initial `si-docker` crate for typed bind mounts, container specs, Docker `run` arg rendering, and preflight mount validation
- completed: runtime path resolution module with Rust parity tests for stale-settings fallback, workspace-root inference, and dyad bundled-vs-repo config discovery
- completed: initial `si-runtime` crate that consumes Rust Docker primitives for codex/dyad core mount planning, with translated Go mount behavior tests

### Phase 3: Read-only command migration

Status: in_progress

Implementation:

- port `version`, `help`, and selected diagnostic/inspection commands.
- introduce a compatibility dispatch layer so Go can delegate specific commands to Rust.
- keep outputs intentionally snapshot-tested.

Testing:

- golden stdout/stderr fixtures for migrated commands.
- exit-code parity tests.
- GitHub Actions smoke invoking both Go and Rust implementations.

Exit criteria:

- at least one user-visible command family runs from Rust in CI and release artifacts.

Progress notes:

- completed: experimental Go-to-Rust compatibility boundary for `si version` and `si help`
- completed: focused Go tests covering fallback, explicit bin selection, repo-local binary discovery, and missing-binary failures
- completed: Rust `providers characteristics` surface with JSON coverage and provider-id alias handling
- completed: Go compatibility bridge for `si providers characteristics`, including `--json` passthrough and focused delegation tests
- completed: Rust provider catalog snapshot helpers with translated alias/capability/probe assertions from Go provider tests
- completed: Rust `codex spawn-plan` CLI surface with binary-level tests for planner output, mount assembly, and env defaults
- completed: Go experimental bridge for `spawn` planning so the shipping `si spawn` path can consume Rust container naming, workdir defaults, core env, and core mount plans without changing the default Go execution path

### Phase 4: Runtime substrate migration

Status: in_progress

Implementation:

- migrate process execution and Docker primitives.
- add typed errors for container state, mount validation, image lookup, and log streaming.
- remove implicit stringly-typed command assembly where possible.

Testing:

- integration tests against local Docker daemon.
- failure-path tests for missing mounts, broken networks, and container-not-found cases.
- log-follow and exec stream tests.

Exit criteria:

- codex/dyad implementations can depend on Rust runtime primitives without re-shelling everything.

Progress notes:

- completed: initial `si-runtime` crate for shared codex/dyad core mount planning on top of Rust Docker primitives, with translated Go mount behavior coverage
- completed: experimental Go `spawn` path integration that consumes Rust-generated core mount plans and deterministic spawn planning data behind the Rust CLI compatibility boundary
- completed: Rust codex container-spec builder on top of the spawn planner, including named-volume, restart-policy, workdir, and shell-command rendering
- completed: experimental Go `spawn` path integration can now consume the Rust codex container spec for env, command, bind mounts, volume mounts, restart policy, network, and working directory
- completed: Rust docker/codex spec now models persistent container execution details needed for cutover (`detach`, `user`, labels, published ports, and non-`--rm` runs)
- completed: Rust `spawn-start` execution path now runs the generated codex container command through `si-process`, with a scriptable docker-bin override for integration-style testing
- completed: experimental Go `spawn` can now delegate actual container startup to Rust `spawn-start`, with Go still owning Fort/bootstrap/session handling, post-start seeding, and `attach` behavior
- completed: experimental Go `remove` can now consume Rust codex removal artifact planning for container/volume naming while Docker listing and Fort cleanup remain in Go
- completed: Rust codex now owns actual container and optional volume teardown execution, and experimental Go single/batch `remove` paths can delegate that Docker teardown while Go retains pre-removal profile lookup and post-removal Fort session cleanup
- completed: experimental Go `start` and `stop` can now delegate the Docker container action to Rust while Go retains post-start inspection, Docker socket setup, and Fort/bootstrap session work
- completed: experimental Go `logs` and `tail` can now delegate Docker log streaming arguments and execution to Rust while preserving the current Go command surface
- completed: Rust docker exec command generation now covers non-interactive codex container execution, and experimental Go `clone` delegates that exec path to the Rust CLI
- completed: experimental Go non-tmux custom codex exec can now delegate Docker exec argument assembly and execution to Rust while interactive shell mode remains on the Go path
- completed: experimental Go codex `list`/`ps` now delegates Docker container listing to Rust for both text and JSON output
- completed: experimental Go container-backed codex `status` can now delegate the app-server exec + parse step to Rust while Go retains container lookup, profile fallback, and final output rendering
- completed: experimental Go `respawn` now delegates deterministic remove-target normalization and ordering to a Rust respawn planner while Go retains interactive profile/container selection and final spawn orchestration
- completed: Rust codex now owns deterministic tmux session naming and launch/resume command assembly, and experimental Go tmux attach can consume that plan while retaining host cwd mapping, tmux session recovery, and final attach behavior
- completed: Rust codex now also owns the report/status tmux launch-command assembly, and Go report/status tmux consumers can reuse that Rust command builder while preserving the existing tmux control flow
- completed: initial `si-dyad` crate with deterministic dyad spawn planning for actor/critic names, mounts, labels, env, default volumes, configs mount, and loop/profile env wiring
- completed: Rust `dyad spawn-plan` CLI surface with binary-level JSON coverage for default naming/volumes and critic-specific configs + loop env assembly
- completed: experimental Go `dyad spawn` can now consume Rust dyad planning for deterministic role/image/network/workspace/configs/volume/forward-port defaults behind the compatibility boundary while container creation remains in Go
- completed: Rust `dyad spawn-spec` now materializes actor/critic container specs, published ports, bind mounts, and command payloads on top of the dyad planner with binary-level JSON coverage
- completed: Rust `dyad spawn-start` now executes the actor and critic container startup commands end to end through `si-process`, backed by fake-docker integration tests and Docker primitive support for dynamic host port binding
- completed: experimental Go `dyad spawn` can now delegate fresh actor/critic container creation to Rust `spawn-start` behind the compatibility boundary while existing-container reuse and drift reconciliation stay on the Go path
- completed: Rust dyad now owns `start`, `stop`, and member-specific `logs` Docker invocation surfaces, and the experimental Go dyad commands can delegate those actions after Go-side name/existence checks
- completed: Rust dyad now owns label-aware `list` and `status` parsing on top of the shared Docker substrate, and the experimental Go dyad read-only commands can delegate to those Rust surfaces
- completed: Rust dyad now owns single-dyad `restart` and `remove` Docker invocation surfaces, while Go intentionally retains the interactive `remove --all` confirmation and batch-removal flow
- completed: Rust dyad now owns member-targeted `exec` and cleanup of stopped dyad containers, while Go intentionally retains the pre-exec mount-policy checks and user-facing cleanup success formatting
- completed: Go `dyad recreate` now reuses the same Rust-compatible single-dyad removal path before falling back into the existing spawn flow, so recreate no longer bypasses the delegated Rust teardown path
- completed: Go `dyad remove --all` now keeps its interactive confirmation UX while routing each per-dyad teardown through the same Rust-compatible removal helper used by single-dyad remove and recreate
- completed: Rust `dyad peek-plan` now owns deterministic container/session naming and attach-command assembly, and experimental Go `dyad peek` consumes that plan while retaining tmux session creation and interactive attach behavior
- completed: initial `si-warmup` crate now owns persisted warmup state loading, legacy version normalization, and `warmup status` rendering, and Go `si warmup status` can consume that Rust state loader while preserving the current Go text output path
- completed: live Go warmup reconcile/status state reads and writes now flow through the Rust warmup state loader/writer when the compatibility boundary is enabled, so Rust owns persisted warmup state normalization for both status and mutation paths
- completed: warmup autostart/disabled marker reads and writes now flow through the Rust warmup crate behind the compatibility boundary, so Go scheduler self-repair and enable/disable paths reuse Rust marker semantics with Go-only legacy fallbacks for cached-auth and legacy-state detection
- completed: Rust warmup now owns the marker-plus-state autostart decision (`disabled` / `marker` / `legacy_state` / `none`), and Go scheduler self-repair now consumes that Rust decision directly before applying its remaining Go-only cached-auth fallback
- completed: experimental Go `si warmup status` now delegates the full command surface to Rust, so the migrated Rust warmup text/json rendering becomes the live status implementation behind the compatibility boundary instead of only supplying state data to Go

### Phase 5: Security/runtime migration

Status: in_progress

Implementation:

- move Fort session lifecycle to a dedicated Rust crate with explicit states:
  - bootstrap required
  - resumable profile session
  - refreshing
  - revoked/expired
  - teardown
- move vault file and trust metadata handling into typed Rust components.
- introduce cross-process locking around refresh/session mutation.

Testing:

- Fort session integration matrix.
- concurrent refresh race tests.
- revoked/expired token tests.
- vault trust/recipient/permission regression tests.

Exit criteria:

- no inline ad hoc refresh logic remains in Go for migrated flows.

Progress notes:

- completed: initial `si-fort` crate with a typed Fort session lifecycle model covering bootstrap-required, resumable, refreshing, revoked, teardown, and closed states
- completed: transition tests for refresh success, unauthorized refresh revocation, and teardown completion on top of the Rust Fort model
- completed: strict persisted Fort session-state read/write handling in Rust with atomic writes, permission checks, whitespace normalization, and RFC3339 expiry parsing/classification tests
- completed: Rust CLI Fort session-state inspection/classification surface for exercising the new persisted-state path end-to-end without changing live Go refresh behavior yet
- completed: initial cross-process Fort session mutation lock in Rust with explicit lock acquisition tests and non-blocking contention coverage
- completed: experimental Go Fort session-state loading can now delegate to Rust `fort session-state show`, shifting a real persisted-state read onto the Rust path behind the compatibility boundary
- completed: experimental Go Fort session reuse now honors Rust session classification, so revoked persisted state can short-circuit reuse before Go attempts refresh
- completed: experimental Go Fort runtime-agent state loading can now delegate to Rust `fort runtime-agent-state show`, moving another persisted-state read out of Go before the refresh loop cutover
- completed: experimental Go Fort runtime-agent step now honors Rust session classification, so revoked persisted state can stop the refresh loop before any network refresh attempt
- completed: experimental Go Fort session-state and runtime-agent-state writes can now delegate to Rust persistence surfaces, moving both persisted-state write paths behind the compatibility boundary
- completed: experimental Go Fort refresh success paths can now delegate persisted session-transition application to Rust, so both codex session refresh and runtime-agent refresh use Rust-owned lifecycle mutation instead of ad hoc Go expiry updates
- completed: experimental Go Fort session close now delegates teardown-state transition to Rust before local cleanup and remote close, extending Rust lifecycle ownership beyond refresh into the shutdown path
- completed: experimental Go unauthorized Fort refreshes now delegate revocation mutation to Rust before persisting state, so dead profile sessions are marked through the Rust lifecycle path instead of leaving stale resumable session ids behind
- completed: Rust Fort now owns persisted session-state and runtime-agent-state clear operations, and the experimental Go runtime-agent/session cleanup paths can delegate those file removals instead of deleting the migrated state files directly
- completed: experimental Go codex bootstrap loading can now delegate persisted Fort bootstrap-view normalization to Rust, moving another live Fort state interpretation path behind the compatibility boundary while preserving Go-owned profile path derivation
- completed: live Go Fort open and refresh flows now reuse the persisted Rust bootstrap-view loader after saving session state, so new-session and refreshed-session bootstrap output no longer reassemble profile/agent/container-host data separately from the migrated Rust interpretation path
- completed: experimental Go profile-session refresh now resolves its Fort refresh host through the Rust bootstrap-view loader instead of reading `state.Host` directly, so the live codex session refresh path shares the same migrated host interpretation used by open, wrapper, runtime-agent, and close flows
- completed: experimental Go `si fort` runtime auth wrapper now resolves profile-scoped `FORT_HOST` through the Rust bootstrap-view path when persisted profile Fort state is present, removing another direct Go interpretation of session host/container-host data while preserving the existing hosted-URL validation rules
- completed: experimental Go `si fort` runtime auth wrapper now also sources `FORT_TOKEN_PATH` and `FORT_REFRESH_TOKEN_PATH` from the Rust bootstrap-view when no explicit host env override is set, keeping host-side token-path export aligned with the migrated Rust interpretation instead of preferring a Go-derived default profile path
- completed: experimental Go Fort runtime-agent refresh and session-close flows now resolve their remote Fort host through the Rust bootstrap-view loader instead of reading `state.Host` directly, so the live background refresh path and remote close path share the same migrated host interpretation as the codex/runtime wrapper
- completed: Fort session close now relies on the Rust-aware bootstrap loader directly instead of first probing raw Go session state for existence, removing another direct persisted-state read from the live teardown path
- completed: initial `si-vault` crate for persisted trust-store state with translated round-trip, missing-file, and normalized path update/delete coverage from the Go vault trust path
- completed: experimental Go vault trust enforcement can now delegate trust-store lookup to Rust behind the existing compatibility boundary, with focused bridge and consumer tests

### Phase 6: Codex/dyad lifecycle migration

Status: in_progress

Implementation:

- migrate `spawn`, `respawn`, `status`, `run`, `exec`, `logs`, `remove`, `warmup`.
- migrate dyad actor/critic orchestration only after codex substrate is stable.
- preserve existing profiles, container names, mounts, and compatibility contracts.

Testing:

- multi-profile spawn/respawn/remove matrix.
- tmux/no-tmux paths.
- workspace/config mount tests.
- offline smoke using fake codex runtime.

Exit criteria:

- Rust runtime can replace the Go path for at least one full lifecycle end to end.

Progress notes:

- completed: initial `si-codex` crate for deterministic spawn planning (profile/name normalization, workspace/workdir defaults, volume naming, env assembly, and runtime mount consumption)
- completed: the shipping Go `spawn` path can now delegate deterministic planning to Rust behind the experimental boundary while Fort/bootstrap/session handling remains in Go
- completed: experimental Go `respawn` now applies the delegated Rust respawn plan back into the live flow for effective container name, profile flag normalization, and ordered remove-target selection instead of using the Rust planner only as advisory remove-target output
- completed: experimental Go codex remove now resolves artifact naming for both single-container and `remove --all` batch paths through a shared Rust-aware remove-plan helper, so batch teardown no longer reconstructs container/volume artifacts purely in Go
- completed: experimental Go codex `start`, `stop`, and `clone` now resolve their target container name through the shared Rust-aware remove-plan helper as well, so the post-action lookup and clone preflight paths no longer reconstruct container naming separately from the migrated artifact boundary
- completed: experimental Go `codex stop` now delegates before any Go container-name lookup, so the migrated Rust action surface owns the live happy path instead of sitting behind redundant Go preflight
- completed: experimental Go `codex start` now also delegates before its action-time Go container-name lookup, while still preserving the Go-owned post-start Fort/bootstrap seeding path after the action returns
- completed: experimental Go `codex logs` and `codex tail` now also delegate before any Go container-name lookup, so the migrated Rust logs surface owns the live happy path instead of sitting behind redundant Go preflight
- completed: experimental Go `codex clone` now also delegates before any Go container lookup/client setup, so the migrated Rust clone path owns the live happy path instead of sitting behind redundant Go Docker preflight
- completed: when Rust codex status retrieval succeeds, the live Go `status` command now also reuses the Rust-rendered text/json output on the happy path instead of reformatting the Rust payload back into Go output structures
- completed: the live Go `status` command now attempts the Rust status path before creating a Go Docker client, while still keeping the existing Go fallback for missing-container/profile/auth cases when the Rust path fails
- completed: experimental Go codex `status` and `report` now resolve their target container lookup through the same shared Rust-aware artifact helper, so additional read-only and tmux-backed flows no longer rebuild container naming independently before entering the migrated Rust-backed status/report paths
- completed: experimental Go codex `run`/`exec`, `logs`, and `tail` now resolve their target container lookup through the shared Rust-aware artifact helper too, so more live action paths no longer bypass the migrated codex artifact naming boundary before executing Rust-backed or Docker-backed flows
- completed: profile-auth/status container preference now resolves through the shared Rust-aware codex artifact helper too, so profile auth sync and volume discovery no longer rebuild their preferred container name purely on the Go path
- completed: experimental Go dyad `stop`, `exec`, and `logs` now resolve member container names through Rust-backed dyad status before falling back to Go naming, so more live dyad actions no longer bypass the migrated runtime lookup boundary
- completed: experimental Go dyad `status` fallback now uses the same resolved member container names end-to-end, so even the non-delegated status rendering path no longer rebuilds actor/critic names after lookup
- completed: experimental Go `dyad status` now delegates the full command surface to Rust behind the compatibility boundary, so the migrated Rust text/json rendering path is live instead of only feeding parsed status data back into Go
- completed: experimental Go `dyad cleanup` now delegates the full command surface to Rust too, so stopped-container cleanup no longer performs a redundant Go Docker preflight before the migrated Rust cleanup path runs
- completed: experimental Go `dyad logs` now delegates the full text/json command surface to Rust too, so member-log rendering no longer depends on Go-side Docker preflight or JSON wrapping around an already-migrated Rust execution path
- completed: experimental Go `dyad start`, `stop`, and `restart` now delegate before any Go Docker client preflight, so the migrated Rust action surface owns the live happy path instead of sitting behind redundant Go existence checks
- completed: single-dyad Go `dyad remove` now delegates before creating a Go Docker client too, so the migrated Rust teardown path owns the live happy path instead of sitting behind redundant Go client setup
- completed: experimental Go dyad `peek` fallback now seeds container/session attach planning from the shared Rust-aware dyad lookup helper too, so the interactive tmux path no longer reconstructs actor/critic container names independently before optional Rust peek-plan delegation
- completed: experimental Go dyad spawn preflight now resolves existing actor/critic container names through the shared Rust-aware dyad lookup helper too, so the reuse-vs-create decision before Rust spawn-start no longer bypasses the migrated runtime naming boundary
- completed: the host-side Fort wrapper session bootstrap fallback now reuses the shared Rust-aware bootstrap loader path too, so runtime env preparation no longer carries a separate Go-only reconstruction of persisted bootstrap host/token details when Rust delegation is unavailable
- completed: Fort session close now uses the shared Rust-aware bootstrap loader directly for remote close host resolution instead of first reading raw session state for a profile id, removing another live teardown-path dependency on parallel Go bootstrap reconstruction
- completed: profile-session refresh now resolves profile identity and bootstrap host data through the shared Rust-aware bootstrap loader before touching raw session state, removing another live refresh-path dependency on separate Go bootstrap reconstruction
- completed: the Fort runtime-agent step now resolves Rust-backed session classification and bootstrap host data before loading raw session state, so no-refresh iterations stay on the migrated interpretation path and only touch persisted state when a refresh mutation must be saved
- completed: live codex-session refresh and runtime-agent refresh now defer raw session-state loading until a Rust transition result actually needs fallback persisted state mutation, reducing direct Go state handling in the hot refresh path when Rust already returns the next lifecycle snapshot
- completed: single-container Go codex remove now resolves its target container name through the shared Rust-aware artifact helper from the start, so the live remove flow no longer seeds with raw Go naming before consulting the migrated remove-plan boundary
- completed: single-container Go `codex remove` can now consume a structured Rust remove result with profile metadata, so the live happy path no longer needs a pre-removal Go inspect just to recover the Fort cleanup profile id
- completed: the shipping Go codex spawn path now seeds its target container name through the shared Rust-aware artifact helper from the start, so live spawn orchestration no longer begins from a separate raw Go naming rule before Rust-aware planning takes over
- completed: the shared Go codex remove-artifact fallback now derives its default container name directly from the canonical slug it already resolved, removing one more internal dependency on the separate raw Go container-name helper inside the migrated artifact boundary
- completed: Rust `codex spawn-spec` surface exposing the next cutover boundary after planning, with JSON tests covering named volumes and command rendering
- completed: Go bridge helpers and focused delegation tests for Rust codex spawn-spec payloads
- completed: Rust `codex spawn-run-args` surface exposing executable docker invocation args for the codex runtime path
- completed: Rust codex now owns prompt segmentation and report extraction for tmux report captures, and the experimental Go report flow can delegate that parsing while preserving tmux polling, prompt submission, and session lifecycle control

### Phase 7: Provider migration

Status: planned

Implementation:

- prioritize providers by complexity and coupling:
  - GitHub
  - Cloudflare/Stripe/WorkOS
  - AWS/GCP/Google/Apple
  - OpenAI/OCI and other high-surface integrations
- each provider gets its own crate or module boundary.

Testing:

- fixture-backed API response tests.
- contract validation for auth/env handling.
- command snapshot tests.

Exit criteria:

- provider surfaces are no longer monolithic files in the main CLI package.

### Phase 8: Release/install migration

Status: planned

Implementation:

- port installer, packager, release-preflight, npm/homebrew helpers.
- keep generated release assets identical or intentionally versioned.

Testing:

- full release-preflight dry runs.
- installer smokes: host, npm, Docker.
- checksum and artifact verification.

Exit criteria:

- release runbook is executable without the old Go/shell implementation path.

### Phase 9: Primary binary cutover

Status: planned

Implementation:

- ship Rust as the main `si` binary.
- package Go compatibility adapters only for unmigrated flows.
- cut over CI, npm, and Homebrew packaging.

Testing:

- full CI green.
- release-candidate soak on tagged builds.
- local install verification from npm and Homebrew.

Exit criteria:

- default install path resolves to the Rust binary.

### Phase 10: Go retirement

Status: planned

Implementation:

- remove dead Go code paths and shell helpers replaced by Rust.
- simplify docs, workflows, and release automation.

Testing:

- grep-based dead-reference validation.
- full regression test suite.
- release dry run and public release verification.

Exit criteria:

- no production `si` command depends on the retired Go implementation.

## Test Strategy by Layer

### Unit tests

- path expansion, settings parsing, version parsing, command manifest construction
- Fort/vault state transitions
- provider request/response mapping

### Snapshot/golden tests

- help output
- read-only command output
- settings/path diagnostics
- provider JSON/text rendering

### Integration tests

- Docker lifecycle for codex/dyad
- Fort spawn/auth matrix
- vault secure env execution
- installer/release/preflight flows

### Compatibility tests

- compare Rust and Go output/exit codes for commands under migration
- compare resolved paths/settings against Go fixtures
- compare runtime side effects where output parity is insufficient

### Release gates

- all Rust lanes green
- existing Go lanes green until their migrated areas are retired
- release-preflight assets generated and verified

## Rollback Rules

- every migrated command family keeps an explicit fallback to the current Go implementation until parity and soak criteria are met.
- no release may remove the prior implementation in the same change that first introduces a migrated replacement.
- cutover requires one full release cycle of successful validation before deleting the old path.

## Immediate Next Actions

Status: in_progress

1. Expand `si-config` from `[paths]` into broader settings parity with Go fixtures.
2. Add a Rust command-manifest crate and snapshot-tested help metadata.
3. Introduce a compatibility dispatch boundary for the first migrated read-only command path.
4. Keep the current Go CLI unchanged until parity harnesses exist for the delegated commands.
