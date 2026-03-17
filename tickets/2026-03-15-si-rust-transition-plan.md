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
| 6. Codex/dyad lifecycle migration | completed | core container lifecycle ports to Rust | spawn/respawn/status/run/remove, tmux/dyad orchestration | container lifecycle matrix, regression parity suite, multi-profile smoke tests |
| 7. Provider migration | in_progress | provider families port incrementally | GitHub first, then low-complexity providers, then high-complexity providers | API contract tests, auth tests, fixture-based command parity |
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

Status: completed

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
- completed: experimental Go `codex stop` now also consumes a structured Rust action result, so the stop happy path no longer depends on the older text-only action bridge shape
- completed: experimental Go `codex start` now also delegates before its action-time Go container-name lookup, while still preserving the Go-owned post-start Fort/bootstrap seeding path after the action returns
- completed: experimental Go `codex start` can now consume a structured Rust action result for the post-start bootstrap path, so the live happy path no longer re-resolves the container name after delegated startup
- completed: experimental Go `codex logs` and `codex tail` now also delegate before any Go container-name lookup, so the migrated Rust logs surface owns the live happy path instead of sitting behind redundant Go preflight
- completed: experimental Go `codex clone` now also delegates before any Go container lookup/client setup, so the migrated Rust clone path owns the live happy path instead of sitting behind redundant Go Docker preflight
- completed: experimental Go `codex clone` can now consume a structured Rust clone result for success reporting, so the live happy path no longer re-resolves the target container name after delegated clone execution
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
- completed: Rust dyad CLI now has an offline fake-docker lifecycle smoke covering spawn-start, status, logs, stop, start, and remove, providing an end-to-end runtime proof for the migrated dyad lifecycle surface
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
- completed: Rust codex CLI now has an offline fake-docker lifecycle smoke covering spawn-start, status-read, logs, stop, start, clone, and remove, providing an end-to-end runtime proof for the migrated codex lifecycle surface
- completed: the shipping Go codex command layer now has a delegated fake-`si-rs` lifecycle smoke covering start, status, logs, clone, stop, and remove, giving the compatibility boundary an end-to-end proof instead of only per-command delegation tests
- completed: the shipping Go `respawn` command now has a focused command-level proof that the delegated Rust respawn plan drives ordered teardown targets, volume passthrough, and the follow-up spawn args instead of remaining an unverified advisory path
- completed: the shipping Go dyad command layer now has a delegated fake-`si-rs` lifecycle smoke covering status, logs, start, stop, restart, remove, and cleanup, giving the compatibility boundary the same end-to-end proof that already exists for the Rust dyad CLI
- completed: the shipping Go codex and dyad `list` commands now have direct delegated command proofs for the migrated Rust text/json list surfaces, not just helper-level bridge tests
- completed: the shipping Go `run --no-tmux` path now has a direct delegated command proof for the migrated Rust codex exec surface, and `dyad exec` has a command-level proof for its parsed argument handoff into the migrated exec seam
- completed: the shipping Go `dyad peek --detached` path now has a direct command-level proof that the Rust peek plan drives the tmux session name and attach-command assembly on the live happy path
- completed: the shipping Go attached `dyad peek` path now also has a direct command-level proof that the Rust peek plan drives the live tmux session selection before attach
- completed: the shipping Go `report` command now has a command-level happy-path proof that the live report flow consumes the migrated Rust parsing surface while lock, lookup, and tmux orchestration remain on the Go side
- completed: the shipping Go warmup command layer now has command-level proofs for `enable`, `disable`, `reconcile`, and delegated `status`, so the remaining migrated warmup state/marker surfaces are exercised through the live CLI and not only via helper-level tests
- completed: the shipping Go `dyad spawn` command now has a command-level proof that the parsed `DyadOptions` flow into execution after the delegated Rust spawn plan has rewritten role/image/runtime fields, instead of leaving live spawn-plan consumption only helper-tested
- completed: the shipping Go `dyad spawn` command now also has a workspace/configs matrix across multiple names, so the Phase 6 mount-path requirement is exercised through delegated Rust plan rewrites instead of only single-name happy paths
- completed: the batch `dyad remove --all` command path now has a direct command-level proof that the live CLI routes into the shared batch teardown flow instead of only leaving batch removal covered implicitly through lower-level helpers
- completed: the batch `remove --all` codex command path now has a direct command-level proof that the live CLI routes into the shared batch teardown flow, matching the single-container and dyad batch teardown command coverage
- completed: the shipping Go `spawn` command now has a direct command-level proof that delegated Rust remove-plan/spawn-plan/spawn-spec rewriting reaches the prepared execution boundary before Fort and Docker orchestration begin
- completed: the shipping Go `dyad recreate` command now has a direct command-level proof that it preserves the delegated teardown path and then re-enters the live spawn flow with the parsed CLI args intact
- completed: the shipping Go `run --tmux` path now has a direct command-level proof that parsed container selection reaches the attached tmux execution boundary before the interactive attach/runtime tail begins
- completed: the shipping Go codex command layer now has a delegated multi-profile spawn/remove/respawn matrix, so profile-specific Rust rewrite behavior is exercised across more than one profile instead of only in single-profile happy paths
- completed: the shipping Go dyad command layer now has a delegated full-lifecycle smoke that starts with Rust `spawn-start` and then runs through `status`, `logs`, `stop`, `start`, and `remove`, satisfying the Phase 6 exit criterion that at least one lifecycle is replaceable end to end through the Rust runtime boundary

### Phase 7: Provider migration

Status: in_progress

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

Progress notes:

- completed: initial `si-rs-provider-github` crate now owns GitHub context-list rendering from settings, giving Phase 7 its first real provider-specific Rust module boundary
- completed: experimental Go `si github context list` now delegates to the Rust provider slice behind the compatibility boundary, with fixture-backed Rust CLI coverage and a live Go command proof
- completed: the GitHub provider slice now also owns `context current` resolution and rendering, so the first Phase 7 auth/context path has moved behind a provider-specific Rust module and live Go delegation path
- completed: the GitHub provider slice now owns `auth status` local auth/context resolution and rendering too, so the first Phase 7 provider auth-source seam has moved behind a provider-specific Rust module and live Go delegation path while the Go fallback still covers the full legacy behavior
- completed: the GitHub provider slice now has explicit OAuth and App auth-source matrix coverage in Rust CLI and provider tests, strengthening the Phase 7 auth/env contract validation lane before moving on to the next provider family
- completed: initial `si-rs-provider-stripe` crate now owns Stripe `context list`, `context current`, and `auth status` local runtime resolution/rendering, giving Phase 7 a second provider family behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: the Stripe provider slice now also owns the operational `raw` and `report` surfaces, with Rust-owned authenticated transport, pagination-backed report aggregation, and Go wrapper delegation so the first broader Stripe runtime lane no longer depends on the Go path
- completed: the Stripe provider slice now also owns read-only `object list|get`, with Rust-owned object registry resolution, pagination-backed listing, direct object retrieval, and Go wrapper delegation so the first broader Stripe resource retrieval lane no longer stays only on the Go path
- completed: the Stripe provider slice now also owns the remaining `object` mutation lane (`create`, `update`, and force-gated `delete`), with Rust-owned CRUD request shaping, object capability enforcement, and Go wrapper delegation so the full generic Stripe object surface now lives behind the Rust compatibility boundary
- completed: the Stripe provider slice now also owns `sync live-to-sandbox plan|apply`, with Rust-owned family parsing, live-vs-sandbox diff planning, payload flattening, sandbox apply execution, and Go wrapper delegation so the high-level Stripe replication workflow no longer depends on the Go path
- completed: initial `si-rs-provider-workos` crate now owns WorkOS `context list`, `context current`, and `auth status` local runtime resolution/rendering, extending Phase 7 to a third low-complexity provider family behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: the WorkOS provider slice now also owns the main runtime/resource lane (`doctor`, `raw`, `organization`, `user`, `membership`, `invitation`, and `directory`), with Rust-owned bearer transport plus Go wrapper delegation so the practical WorkOS API surface no longer remains only on the Go path apart from the public doctor probe
- completed: initial `si-rs-provider-cloudflare` crate now owns Cloudflare `context list`, `context current`, and `auth status` verification, extending Phase 7 with the next low-complexity provider family behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: initial `si-rs-provider-apple` crate now owns Apple App Store `context list` and `context current` local runtime resolution/rendering, extending Phase 7 into the next provider tier behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: initial Apple App Store `auth status` local runtime resolution/rendering now lives in the Rust provider slice and Rust CLI, while the shipping Go command keeps default `--verify` execution on the Go path and only delegates the non-verifying compatibility path to Rust until the verification probe is migrated
- completed: initial `si-rs-provider-aws` crate now owns AWS `context list`, `context current`, and local `auth status` runtime resolution/rendering, extending Phase 7 into the cloud-provider tier behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: the AWS provider slice now also owns the signed IAM verification/runtime lane, with Rust-owned SigV4 query transport plus verified `auth status`, default signed `doctor`, and Go wrapper delegation while the separate `doctor --public` probe remains explicitly on the Go path
- completed: the AWS provider slice now also owns the first real resource lane on top of that signer, covering `sts get-caller-identity`, `sts assume-role`, `iam user create`, `iam user attach-policy`, and generic `iam query`, with Rust-owned query execution plus Go wrapper delegation so the first non-diagnostic AWS service families no longer depend on the Go path
- completed: the AWS provider slice now also owns the next service batch behind the Rust boundary, covering `s3 bucket list|create` plus force-gated `delete` and `ec2 instance list` plus force-gated `start|stop|terminate`, with Rust-owned REST/query execution and Go delegation that keeps the old prompt-driven mutation path as fallback until `--force` is explicit
- completed: the AWS provider slice now also owns the next higher-surface service batch behind the Rust boundary, covering `lambda function list|get` plus force-gated `delete` and `ecr repository list|create` plus force-gated `delete` together with `ecr image list`, with Rust-owned REST/JSON-target execution and Go delegation that preserves the old prompt-driven destructive path until `--force` is explicit
- completed: the AWS provider slice now also owns the next transport-adjacent service batch behind the Rust boundary, covering the full `s3 object` lane plus `secrets list|get|create|put` with force-gated `delete` and the `kms key`, `encrypt`, and `decrypt` lanes, preserving file IO behavior for object get/put and explicit `--force` fallback rules where the prior Go path still handled interactive confirmation
- completed: the AWS provider slice now also owns the remaining service families behind the Rust boundary, covering `dynamodb`, `ssm`, `logs`, and `cloudwatch metric list`, so the practical AWS command surface now runs through Rust apart from the intentional Go fallbacks for prompt-driven destructive flows and any still-unmigrated Bedrock lane
- completed: the AWS provider slice now also owns the first Bedrock discovery lane behind the Rust boundary, covering `foundation-model`, `inference-profile`, and `guardrail` list/get with Rust-owned signed REST execution plus Go wrapper delegation, leaving the broader Bedrock runtime, jobs, agent, and knowledge-base flows for the next explicit Phase 7 slices
- completed: the AWS provider slice now also owns the Bedrock runtime lane behind the Rust boundary, covering `runtime invoke`, `runtime converse`, and `runtime count-tokens` with Rust-owned signed runtime REST execution, prompt/body/body-file payload parity, and Go compat delegation while the remaining Bedrock jobs, agent, knowledge-base, and agent-runtime trees stay queued for the next explicit Phase 7 slices
- completed: the AWS provider slice now also owns the remaining Bedrock lane behind the Rust boundary, covering `job`, `agent`, `knowledge-base`, and `agent-runtime` with Rust-owned signed REST execution, nested alias parity, force-gated `job stop`, and Go bridge delegation so the practical AWS Bedrock subtree is now fully migrated apart from the existing intentional Go fallbacks for prompt-driven destructive flows elsewhere
- completed: initial `si-rs-provider-gcp` crate now owns GCP `context list`, `context current`, and local `auth status` runtime resolution/rendering, extending Phase 7 further into the cloud-provider tier behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: initial `si-rs-provider-google` crate now owns Google Places `context list`, `context current`, and local `auth status` runtime resolution/rendering, extending Phase 7 into the broader Google provider tier behind the Rust compatibility boundary with focused Rust and Go command proofs
- completed: the Google Places provider slice now also owns the core networked search/retrieval lane (`autocomplete`, `search-text`, `search-nearby`, `details`, and photo metadata/download without redirect-follow mode), with Rust-owned API-key transport, field-mask handling, paginated search aggregation, and Go wrapper delegation so the first higher-surface Google Places runtime path no longer depends on the Go command implementation
- completed: the Google Places provider slice now also owns the remaining runtime escape hatches (`doctor` and `raw`), with Rust-owned verification probes and generic API request execution plus Go fallback only for the separate `doctor --public` probe path
- completed: the remaining Google Places local utility lane (`session`, `types`, and `report`) now also runs through the Rust CLI compatibility boundary, preserving the existing session-store path and report/log-file conventions so the Google Places subtree is effectively closed behind the Rust path apart from the explicit Go-only `doctor --public` and `photo get --follow` fallbacks
- completed: the Google provider slice now also owns the initial YouTube runtime/read-only lane (`context list|current`, `auth status`, default signed `doctor`, `search list`, `support languages|regions|categories`, and `raw`), with Rust-owned YouTube runtime resolution, API-key/OAuth request transport, OAuth token-store compatibility, and Go wrapper delegation that keeps broader YouTube resource/mutation families plus the separate `doctor --public` probe on the Go path for now
- completed: the Google provider slice now also owns the broader Google Play runtime and publishing lane (`context list|current`, `auth status`, default signed `doctor`, `raw`, custom-app `app create`, `listing get|list|update`, `details get|update`, `asset list|upload|clear`, `release upload|status|promote|set-status|halt|resume`, and `apply`), with Rust-owned service-account JWT exchange, Android Publisher/custom-app transport, edit lifecycle handling, media upload support, metadata bundle loading, and Go wrapper delegation while the separate `doctor --public` probe remains on the Go path for now
- completed: the Google provider slice now also owns the core YouTube read lane (`channel list|get|mine`, `video list|get`, `playlist list|get`, and `playlist-item list`) on top of the existing Rust YouTube runtime/search/support transport, with Go wrapper delegation for those retrieval subtrees while the broader OAuth mutation, upload, subscription/comment, caption/thumbnail, live, and reporting lanes remain queued for later Phase 7 slices
- completed: initial `si-rs-provider-openai` crate now owns OpenAI `context list` and `context current` local runtime resolution/rendering, starting the higher-surface OpenAI/OCI tier behind the Rust compatibility boundary while leaving verification-heavy `auth status` on the Go path for now
- completed: initial `si-rs-provider-oci` crate now owns OCI `context list` and `context current` local runtime resolution/rendering, extending the higher-surface OpenAI/OCI tier behind the Rust compatibility boundary while leaving verification-heavy `auth status` on the Go path for now
- completed: initial OCI `auth status` local runtime resolution/rendering now lives in the Rust provider slice and Rust CLI, while the shipping Go command keeps default `--verify` execution on the Go path and only delegates the non-verifying compatibility path to Rust until OCI request-signing verification is migrated
- completed: Rust now also owns the read-only `oci oracular tenancy` surface on top of the migrated OCI context resolver, extending the OpenAI/OCI provider tier with another live delegated command while request-signing-heavy OCI API calls remain on the Go path
- completed: the OCI provider slice now also owns the next operational bootstrap lane (`oracular cloud-init`, identity availability-domains and compartment create, compute image lookup and instance create, plus network VCN/internet-gateway/route-table/security-list/subnet creation), with Rust-owned request signing/transport plus Go wrapper delegation so the main OCI bootstrap workflow no longer remains only on the Go path
- completed: the OCI provider slice now also owns request-signing verification for `auth status --verify` plus the `oci raw` escape hatch, with Rust-owned signed/unsigned request execution and Go wrapper delegation so the remaining OCI runtime surface is no longer split by verification mode
- completed: the default signed `oci doctor` flow now also runs through the Rust OCI provider runtime and CLI, preserving the Go fallback only for the separate `--public` unauthenticated probe path while closing the main OCI readiness-check seam behind the Rust boundary
- completed: the OpenAI provider slice now also owns read-only `model list` and `model get` API execution on top of Rust-resolved auth/context headers, extending Phase 7 into the first higher-surface networked OpenAI operation while broader project/key/admin flows stay on the Go path
- completed: the OpenAI provider slice now owns API-mode `auth status` verification too, so Rust covers both local context resolution and the standard OpenAI readiness probe while Codex-profile auth status remains on the Go path
- completed: the OpenAI provider slice now also owns the default signed `doctor` readiness flow, and the Go compatibility bridge now correctly keeps Codex-mode `auth status` on the legacy path instead of misrouting unsupported `--auth-mode codex` flags into the Rust CLI
- completed: the OpenAI provider slice now owns read-only admin-key `project list` and `project get` execution too, extending Phase 7 deeper into the OpenAI organization API while create/update/archive and nested project-admin flows remain on the Go path
- completed: the OpenAI provider slice now owns read-only project `api-key list` and `api-key get` execution too, extending Phase 7 deeper into OpenAI project-admin retrieval flows while delete/create/mutation paths remain on the Go path
- completed: the OpenAI provider slice now owns read-only project `service-account list` and `service-account get` execution too, extending Phase 7 through the remaining project-admin retrieval flows while creation/deletion and broader mutation paths remain on the Go path
- completed: the OpenAI provider slice now owns read-only project `rate-limit list` execution too, extending Phase 7 across the remaining OpenAI project-admin listing surfaces while update/mutation flows remain on the Go path
- completed: the OpenAI provider slice now owns read-only top-level admin `key list` and `key get` execution too, extending Phase 7 across the remaining non-mutating OpenAI admin-key retrieval surfaces while create/delete and broader mutation paths remain on the Go path
- completed: the OpenAI provider slice now also owns the bounded admin/project mutation lane (`project create|update|archive`, `project rate-limit update`, `project api-key delete`, `project service-account create|delete`, and `key create|delete`), with Rust-owned request shaping plus Go wrapper delegation so the remaining non-raw OpenAI admin surface now lives behind the Rust provider boundary
- completed: the OpenAI provider slice now owns `raw` request execution too, including custom headers, query params, raw or JSON bodies, and admin-key routing, so the remaining OpenAI escape-hatch transport no longer needs the Go wrapper path
- completed: the OpenAI provider slice now owns read-only `usage <metric>` execution too, extending Phase 7 across the shared OpenAI usage/monitoring retrieval surface while mutation paths and broader raw/admin write flows remain on the Go path
- completed: the OpenAI provider slice now also owns read-only `codex usage`, reusing the migrated completions-usage path with Codex-default model filtering so another live OpenAI monitoring surface no longer stays only on the Go wrapper path
- completed: the OpenAI provider slice now also owns the read-only `monitor` wrapper surface, preserving Go-compatible `usage` defaulting and `limits` routing while removing another top-level monitoring command from the remaining Go wrapper seam
- completed: the Go compatibility layer no longer owns top-level `openai usage` or `openai codex` wrapper parsing either, so those read-only routing surfaces now dispatch directly into the already-migrated Rust command tree
- completed: the top-level read-only `openai model` wrapper now dispatches directly into Rust as well, shrinking the last pure OpenAI retrieval wrapper seam on the Go side while preserving the existing fallback path
- completed: the top-level `auth` wrappers for AWS, GCP, and Google Places now dispatch directly into the migrated Rust command trees, while Apple App Store and OCI auth wrappers also route through Rust whenever their existing `--verify=false` compatibility path is selected
- completed: the top-level `context` wrappers for OpenAI, AWS, GCP, Google Places, Apple App Store, and OCI now route `list` and `current` directly into Rust while keeping the mutable `use` subcommand on the Go side
- completed: the provider-root wrappers for AWS, GCP, Google Places, and Apple App Store now short-circuit directly into Rust whenever the request stays within already-migrated auth/context subtrees, reducing another layer of Go-only routing without disturbing the remaining Go-owned commands
- completed: the `openai` and `oci` provider-root wrappers now also short-circuit directly into Rust for already-migrated read-only subtrees, including OpenAI admin/project retrieval flows and OCI tenancy inspection, while leaving write paths and non-migrated OCI API families on the Go side
- completed: the remaining nested `openai` and `oci` wrapper layers now also delegate migrated read-only subcommands into Rust, so list/get/status monitoring and tenancy-inspection paths no longer need to traverse extra Go-only routing shells before hitting the Rust compatibility boundary
- completed: the GitHub provider root plus its `auth` and `context` wrappers now also short-circuit directly into Rust for the already-migrated read-only status/current/list surfaces, leaving the larger repo/project/workflow mutation families on the Go side until their Phase 7 slices are explicitly migrated
- completed: the GitHub provider slice now also owns read-only `release list` and `release get` execution, including OAuth and GitHub App token flows in Rust plus live Go delegation through the release wrapper layer, while release create/upload/delete paths remain on the Go side
- completed: the GitHub provider slice now also owns the remaining `release` mutation lane (`create`, `upload`, and `delete`), including notes-file ingestion, upload-url expansion, release-id resolution for delete, and Go wrapper delegation so the full `github release` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns the `secret` subtree (`repo|env|org set|delete`), including public-key lookup, sealed-box encryption of secret values, org visibility/repository targeting, and Go wrapper delegation so GitHub secret management now lives behind the Rust provider boundary too
- completed: the GitHub provider slice now also owns read-only `repo list` and `repo get` execution, including Rust-owned pagination and runtime auth resolution plus live Go delegation through the repo wrapper layer, while create/update/archive/delete stay on the Go side
- completed: the GitHub provider slice now also owns the remaining `repo` mutation lane (`create`, `update`, `archive`, and `delete`), with Rust-owned REST execution, force-gated archive/delete parity, and Go wrapper delegation so the full `github repo` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns read-only `project list` and `project get` execution on top of Rust-owned GraphQL auth/runtime handling, with Go wrapper delegation for those retrieval paths while project updates, fields/items reads, and item mutations remain on the Go side
- completed: the GitHub provider slice now also owns read-only `project fields` and `project items` retrieval, with Rust-owned GraphQL resolution plus Go wrapper delegation for those read paths while project updates and item mutation flows remain on the Go side
- completed: the GitHub provider slice now also owns the remaining `project` mutation lane (`update`, `item-add`, `item-set`, `item-clear`, `item-archive`, `item-unarchive`, and `item-delete`), with Rust-owned GraphQL execution and field-resolution helpers so the full `github project` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns read-only workflow retrieval (`workflow list`, `workflow runs`, and `workflow run get`), with Rust-owned Actions API execution plus Go wrapper delegation for those read paths while dispatch/cancel/rerun/logs/watch remain on the Go side
- completed: the GitHub provider slice now also owns read-only `workflow logs`, with Rust-owned Actions log retrieval plus Go wrapper delegation for that read path while dispatch/cancel/rerun/watch remain on the Go side
- completed: the GitHub provider slice now also owns the remaining workflow operational lane (`workflow dispatch`, `workflow run cancel`, `workflow run rerun`, and `workflow watch`), with Rust-owned Actions execution/polling plus Go wrapper delegation so the full `github workflow` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns read-only `issue list|get` and `pr list|get` execution, with Rust-owned pagination and retrieval over the REST API plus Go wrapper delegation for those read paths while create/comment/state-change/merge flows remain on the Go side
- completed: the GitHub provider slice now also owns the remaining `issue` mutation lane (`create`, `comment`, `close`, and `reopen`), with Rust-owned REST execution plus Go wrapper delegation so the full `github issue` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns the remaining `pr` mutation lane (`create`, `comment`, and `merge`), with Rust-owned REST execution plus Go wrapper delegation so the full `github pr` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns read-only `branch list|get` execution, including protected-filter handling and escaped branch-name retrieval in Rust plus Go wrapper delegation for those read paths while branch creation/deletion/protection changes remain on the Go side
- completed: the GitHub provider slice now also owns the remaining branch mutation lane (`branch create`, `branch delete`, `branch protect`, and `branch unprotect`), including default-branch SHA resolution, branch-protection payload shaping, and Go wrapper delegation so the full `github branch` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns the remaining `git` helper lane (`credential get`, `setup`, `remote-auth`, and `clone-auth`), with Rust-owned local repo scanning, remote normalization/rewrites, helper command generation, and auth token resolution plus Go wrapper delegation so the full `github git` subtree now lives behind the Rust provider boundary
- completed: the GitHub provider slice now also owns read-only escape-hatch retrieval through `github raw` GET requests and query-only `github graphql`, with Rust-owned runtime/auth execution and Go wrapper delegation for those read paths while mutating raw/graphql traffic remains on the Go side
- completed: Stripe, WorkOS, and Cloudflare now follow the same pattern too, with provider-root plus `auth` and `context` wrapper layers short-circuiting directly into Rust for their already-migrated read-only paths while broader resource and mutation families remain on the Go side
- completed: the Cloudflare provider slice now also owns the `raw` escape hatch, with Rust-owned auth/runtime resolution, direct API transport, and Go wrapper delegation so the first broader Cloudflare operational command no longer depends on the Go path
- completed: the Cloudflare provider slice now also owns the read-only `analytics` and `report` operational surfaces, reusing the migrated Rust transport plus Go wrapper delegation so the next broader Cloudflare runtime reads no longer stay only on the Go path
- completed: the Cloudflare provider slice now also owns `smoke`, reusing the migrated Rust transport for the multi-endpoint read-only health matrix plus Go wrapper delegation so the next operational readiness surface no longer depends on the Go path
- completed: the Cloudflare provider slice now also owns read-only `logs received`, reusing the migrated Rust transport plus Go wrapper delegation so the next narrow operational retrieval surface no longer stays only on the Go path
- completed: the Cloudflare provider slice now also owns the full `logs job` subtree (`list`, `get`, `create`, `update`, and force-gated `delete`), with Rust-owned list pagination, JSON-body shaping, and Go wrapper delegation so Cloudflare logpush job management no longer depends on the Go path
- completed: the Cloudflare provider slice now also owns the next broad resource-family batch behind the Rust boundary, covering `zone`, `dns`, `email rule`, `email address`, `token`, `ruleset`, `firewall`, `ratelimit`, `workers script`, `workers route`, `pages project`, and `queue`, with shared Rust CRUD/pagination handling plus Go wrapper delegation so the main Cloudflare REST resource lanes no longer depend on the Go path
- completed: the Cloudflare provider slice now also owns another remaining resource batch behind the Rust boundary, covering `waf` read/update plus `r2 bucket`, `d1 db`, `kv namespace`, `access app`, `access policy`, `tunnel`, and `tls cert`, reusing the shared Rust CRUD layer plus Go wrapper delegation so most remaining Cloudflare account/zone management surfaces no longer depend on the Go path
- completed: the Cloudflare provider slice now also owns the remaining operational subtrees behind the Rust boundary, covering `token verify|permission-groups`, `email settings`, `workers secret`, `pages deploy|domain`, `r2 object`, `d1 query|migration`, `kv key`, `tunnel token`, `tls get|set|origin-cert`, `lb|lb pool`, and `cache purge|settings`, reusing the shared Rust transport plus focused custom helpers so the practical Cloudflare command surface is now behind Rust apart from the existing explicit fallback rules on force-gated destructive flows
- completed: the top-level `apple` and `google` roots now also short-circuit directly into Rust when routing into the already-migrated `appstore` and `places` subtrees, removing the last obvious outer wrapper layer above those provider families while leaving other Apple/Google surfaces on the Go side
- completed: the Apple App Store provider slice now also owns the main resource and metadata lane behind the Rust boundary, covering `app list|get|create`, `listing get|update`, `raw`, and metadata-bundle `apply`, with Rust-owned JWT transport, App Store Connect request helpers, and Go wrapper delegation while `doctor` and auth verification still remain on the Go side for now
- completed: the Apple App Store provider slice now also owns the default verification lane, with Rust-owned `auth status --verify` and signed `doctor` execution plus Go wrapper delegation, while the unauthenticated `doctor --public` probe still stays on the Go side for now
- completed: the GCP provider slice now also owns the root Service Usage runtime lane (`doctor`, `service enable|disable|get|list`, and `raw`), with Rust-owned bearer-token transport, direct Service Usage request execution, and Go wrapper delegation while preserving the separate unauthenticated `doctor --public` probe on the Go side
- completed: the GCP provider slice now also owns the full API Keys lane (`apikey list|get|create|update|delete|lookup|undelete`), with Rust-owned API Keys transport routing, resource-name expansion, JSON-body shaping, and Go wrapper delegation while preserving the existing `--force` safety gate on destructive restore/delete operations
- completed: the GCP provider slice now also owns the full IAM lane (`iam service-account`, `iam service-account-key`, `iam policy`, and `iam role`), with Rust-owned IAM and Cloud Resource Manager request routing, service-account/resource normalization, policy default-resource fallback, and Go wrapper delegation while preserving the existing `--force` safety gates on destructive IAM operations
- completed: the GCP provider slice now also owns the full Gemini lane (`gemini models`, `generate`, `embed`, `count-tokens`, `batch-embed`, `image generate`, and `raw`), with Rust-owned Gemini API-key resolution and OAuth fallback, model-name normalization, inline image extraction/writes, and Go wrapper delegation for both direct `gcp gemini` and `gcp ai gemini` entry points
- completed: the GCP provider slice now also owns the full Vertex lane (`vertex model`, `endpoint`, `batch`, `pipeline`, `operation`, and `raw`), with Rust-owned location/base-url resolution, Vertex resource-name normalization, request-body shaping, force-gated destructive operations, and Go wrapper delegation for direct `gcp vertex` entry points

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
