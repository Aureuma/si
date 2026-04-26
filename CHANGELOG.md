# Changelog

All notable changes to this project will be documented in this file.

## Changelog Guidelines
- Follow Semantic Versioning (SemVer) and keep entries newest-first.
- Use these sections when applicable: Added, Changed, Fixed, Removed, Security.
- Write short, user-facing bullets in past tense.
- Dates use UTC in YYYY-MM-DD.
- Pre-1.0: bump the minor version for feature sets; use patch releases for fixes.
- Note: Entries before v0.39.1 reference the legacy `si codex ...` namespace.

## [Unreleased]
### Changed
- Bumped the working version to `0.59.22` after untracking local ticket documents and keeping `tickets/` ignored.
- Removed the completed Nucleus task hardening and transition hardening ticket documents after implementation and validation.

### Fixed
- Fixed Nucleus hardening regression coverage so worker-restart scope tests seed the intended profile lane and direct-run failure smokes no longer assert unrelated concurrent task completion.
- Fixed Nucleus task execution so the default task ceiling is the real runtime ceiling again; deep tasks no longer fail after a hidden 900-second idle cutoff when they did not ask for one.
- Fixed Nucleus run-failure projection so runtime-emitted `run.failed` events now quarantine timed-out sessions and worker transport failures the same way direct runtime errors already do.
- Fixed Nucleus profile scheduling so tasks pinned to an explicit profile no longer spill onto other profiles when the requested profile is already busy in the current dispatch pass.
- Hardened Nucleus run failure recovery so timed-out or transport-broken turns break the bound session immediately, worker-channel failures also quarantine the worker, and repeated reuse of poisoned sessions now blocks instead of failing again later.
- Hardened Nucleus daemon workdir selection so deleted or invalid current directories fall back to a stable absolute path instead of `.`.
- Throttled repeated Nucleus background-loop warnings so one broken dependency no longer floods the canonical event ledger every 200ms.
- Fixed the Homebrew installer smoke to exercise a real local tap flow, matching current Homebrew tap requirements.
- Hardened the Homebrew tap release workflow to retry `homebrew-si` pushes with `GH_PAT_AUREUMA` when a dedicated tap token can clone but cannot push.
- Fixed Nucleus runtime timeout handling so task and turn submissions now pass `timeout_seconds` through to the runtime instead of always timing out after 15 minutes of silence.
- Fixed Nucleus canonical event-log recovery so malformed JSONL ledgers are quarantined during startup and live iteration instead of repeatedly breaking recovery.
- Fixed Nucleus profile resolution so explicit unknown profiles block immediately as `profile_unavailable`, and fresh no-profile tasks can discover local Codex profiles without requiring a pre-existing worker.
- Fixed the Nucleus OpenAPI maintenance path by adding the missing `si-sync-nucleus-openapi` generator and regenerating the checked-in GPT Actions schema from the canonical runtime document.
- Fixed Nucleus task intake so blank create requests are rejected as invalid params and deterministic invalid session-bound tasks return blocked immediately instead of pretending to queue first.
- Fixed Nucleus task/session binding evaluation so task intake, queued dispatch, and direct run submission now share the same deterministic session checks, including immediate blocking and session breakage for missing app-server thread ids.
- Fixed `run.submit_turn` so tasks already bound to one session cannot be submitted against a different session id.
- Fixed the public Nucleus cancel-task contract to describe `cancellation_requested` as a live runtime interrupt signal rather than a generic state-change flag.
- Fixed Nucleus task retry handling so `max_retries` is now enforced instead of ignored, retried tasks expose `attempt_count` and `session_binding_locked`, unlocked tasks drop broken session affinity before retry, and explicit session-bound tasks do not silently hop to a different session.

## [v0.59.0] - 2026-04-20
### Added
- Added a ReleaseMind-backed GitHub release flow in `si orbit releasemind` with browser-based auth, repo inference from the current GitHub checkout, and release create/view/publish commands.
- Added SI distribution doctor and release-preflight hardening so the CLI release path has a clearer local verification lane before publishing.

### Changed
- Changed the ReleaseMind release-create CLI to align more closely with `gh` by leading with `--repo`, accepting explicit `--generate-notes`, and keeping `--repo-ref` as a compatibility alias.
- Removed stale `.sops.*` gitignore exceptions now that SI uses the native `si vault`/Fort secret path instead of a SOPS+age repo workflow.
- Reverted the SI-local release-bundle helper path so ReleaseMind integration can land through a dedicated orbit client instead of new SI-owned release logic.
- Added a Fort-backed `si surf` noVNC password injection path so `si surf start` can use a stable viewer secret without storing it in plaintext Surf config.
- Fixed SI surf-wrapper settings merging so metadata-only `~/.si/surf/si.settings.toml` files no longer wipe Fort-backed surf wrapper configuration from the core settings file.

### Fixed
- Updated the Nucleus architecture ticket to use accepted-state wording now that all tracked phases are closed.
- Fixed Nucleus validation stability by returning pruned task ids in deterministic order and using the live event-ledger retry reader in runtime-backed id-boundary coverage.
- Fixed Nucleus cleanup paths so empty hook configuration no longer replays malformed event history and cancelled terminal tasks can be pruned with other old terminal work.
- Fixed `si-nucleus` startup so duplicate state-dir owners, failed gateway binds, and accidental arguments cannot start runtime loops that write to the Nucleus state root without owning the listener.
- Fixed Nucleus dispatch so unprofiled tasks that cannot be routed are blocked with an explicit profile-unavailable reason, missing-session tasks surface immediately, and recoverable tasks are re-queued once a single profile can be inferred.
- Fixed `si codex respawn` to behave as the same remove-then-spawn lifecycle as running `si codex remove` followed by `si codex spawn`.
- Fixed Cloudflare direct orbit calls so zone-scoped TLS, tiered cache, DNS, origin certificate, and R2 audit paths no longer require raw URL workarounds.
- Fixed AWS IAM and OCI orbit audit coverage by adding direct read commands for managed-user and core OCI infrastructure resources, and by tolerating OCI private-key files with trailing non-PEM labels.

## [v0.57.0] - 2026-04-08
### Changed
- Bumped the minor release after wiring `si` to the Releasemind release-runbook workflow for GitHub Releases and downstream asset distribution.
- Changed `si codex spawn` to launch Codex with the approvals-and-sandbox bypass flag required by the current worker runtime.

### Fixed
- Fixed `si codex profile list` and related profile displays so a failed live quota probe no longer downgrades a valid logged-in profile to `Missing`.
- Fixed `si fort` wrapper refresh errors to report generic Fort session refresh failures instead of incorrectly blaming bootstrap auth in every case.

## [v0.56.0] - 2026-04-06
### Changed
- Bumped the minor release after completing the current Nucleus architecture and live contract verification set.

## [v0.55.12] - 2026-04-04
### Changed
- Bumped the patch release after removing the Docker runtime path and standardizing on local Codex workers.

## [v0.55.2] - 2026-03-24
### Changed
- Changed the main operational CLI surface to prefer single-word command names such as `spawnplan`, `releaseassets`, `statusread`, and `itemadd`, while keeping the previous hyphenated forms as compatibility aliases.
- Changed repo-owned release helpers, installer wrappers, host-matrix scripts, and docs to use the normalized single-word command names by default.

### Fixed
- Fixed root `si -v` and `si --version` handling so the installed binary reports the current release version directly.

## [v0.55.0] - 2026-03-21
### Added

### Changed
- Changed SI to a Rust-only workspace for build, test, runtime, wrapper, and release flows, with `si-rs` now serving as the primary shipped CLI binary.
- Changed installer, npm, Homebrew, release-asset, and image-build paths to use the Rust-owned self-build and packaging surfaces by default.

### Fixed
- Fixed stable-image compatibility for SI runtime tools and agents.
- Fixed remaining host-wrapper execution and sibling-wrapper fallback issues after the Rust cutover.
- Fixed Google YouTube CLI flag collisions in the caption upload and support categories command paths.

## [v0.54.0] - 2026-03-14
### Added
- Added a profile-scoped Fort runtime refresher that owns steady-state access-token rotation for spawned SI runtimes.

### Changed
- Changed `si spawn`/`si respawn` Fort session recovery to resume usable profile session state first, then ensure the dedicated refresher is running, instead of refreshing inline inside wrapper commands.
- Changed `si fort` wrapper behavior and docs to use file-based runtime auth state only, with refresh ownership delegated to the profile-scoped refresher.
- Changed Fort session state persistence to use strict regular-file validation and atomic state-file writes.

### Fixed
- Fixed cross-process Fort refresh races by adding a per-profile runtime lock around session reuse, refresh, refresher startup, and teardown.
- Fixed `si remove` and `si logout --all` to close and clean up SI-managed Fort runtime sessions when the last profile runtime is torn down.
- Fixed runtime auth drift in docs/help so the published contract matches the Fort-guideline-aligned implementation.

## [v0.53.3] - 2026-03-14
### Changed
- Changed npm install guidance to prefer a user-owned global prefix so SI no longer defaults users back into stale `/usr/local/bin` installs.
- Changed release/testing docs to include the npm installer smoke lane in the standard verification stack.

### Fixed
- Fixed the primary documented npm install path so shells pick up the newly installed SI CLI instead of continuing to launch an older root-owned binary.

## [v0.53.2] - 2026-03-14
### Changed
- Changed CLI release automation to fall back to `GH_PAT_AUREUMA` when the dedicated Homebrew tap token is unavailable, keeping Homebrew distribution updates on the automated release path.

### Fixed
- Fixed `si spawn` first-run workspace inference to prefer the surrounding workspace root over a repo checkout path, avoiding invalid bind mounts when SI is launched from inside the `si` repository.
- Fixed local npm release packaging to fall back from `rename` to copy-and-remove when the staging directory and output directory are on different filesystems.

## [v0.53.1] - 2026-03-14
### Added

### Changed
- Changed workspace and config resolution to prefer `~/.si/settings.toml` path settings, infer sensible defaults from the current repo when possible, and prompt to persist those defaults during interactive first use.
- Changed warmup auto-repair to treat cached logged-in profiles as an opt-in signal and to persist the scheduler marker when that fallback path is used.
- Removed the retired legacy platform/cloud command families, their docs/workflows/tests, and the obsolete backend checkout.

### Fixed
- Fixed shared CLI utility coverage after removing the legacy command families by relocating common helpers into neutral shared code.

## [v0.52.0] - 2026-03-09
### Added
- Added `fort-vault-ci` workflow preflight checks for required Fort cross-repo credentials and repository access before integration execution.

### Changed
- Changed SI Vault backend resolution to strict Fort-only mode (`vault.sync_backend` / `SI_VAULT_SYNC_BACKEND` accept only `fort`).
- Changed vault credential hydration and secret-read helpers to resolve from SI Vault dotenv files instead of legacy Sun KV compatibility paths.
- Changed SI↔Fort spawn-matrix integration harness to be Fort-CLI-version-compatible across auth flag models (`--token-file` and legacy `--token`).

### Removed
- Removed legacy SI vault Sun-compatibility code paths (`vault_sun_backend.go`, `vault_sun_kv.go`) from the vault secret path.

### Fixed
- Updated vault-focused tests and docs to align with Fort-only SI Vault architecture and reject legacy backend aliases.
- Fixed Fort bootstrap-agent setup during `si spawn`/`si respawn` to auto-refresh expired bootstrap admin tokens instead of failing with 401.
- Fixed host installer smoke CI failures on macOS by explicitly setting up Go before running installer smoke scripts.
- Fixed Fort builds invoked from SI to run with `GOWORK=off`, preventing workspace contamination from parent `go.work` state.

### Security
- Hardened Fort CI checkout/auth flow to require explicit secret-backed cross-repo access (`GH_PAT_AUREUMA`) with fail-fast validation.

## [v0.51.0] - 2026-03-01
### Added
- Added per-key vault KV sync (`vault_kv.<scope>/<KEY>`) with revision-history support via `si vault history`.
- Added `cloud_kv` reporting in `si vault status` and KV metadata in `si vault sync status`.

### Changed
- Changed `si vault get`, `si vault list`, and `si vault run` to prefer remote KV reads when available, with local vault fallback.
- Changed `si vault sync push` to mirror dotenv entries into remote KV objects in addition to backup snapshot objects.
- Improved vault command flag UX by allowing mixed flag order for `si vault history` and `si vault unset`.

## [v0.50.0] - 2026-02-27
### Added
- Added remote per-key vault KV mirroring (`vault_kv.<scope>/<KEY>`) with revision history via `si vault history`.
- Added the `si viva` wrapper command and migrated browser runtime integration to `surf bridge`.

### Changed
- Modernized vault crypto behavior to SI Vault native file encryption backed by remote keys for portability.
- Enforced a single remote scope behavior in vault cloud workflows to keep remote data consistent.

### Fixed
- Fixed vault flag parsing for `si vault get` and `si vault unset` with trailing/mixed-order flags.
- Fixed environment inference from dotenv filenames in prod/dev vault flows.
- Fixed npm publish verification flakiness in release automation by adding retry handling.
- Fixed compatibility handling for legacy `si-v1` and `si-v2` vault formats.

### Removed
- Removed legacy command surfaces in vault/dotenv workflows to simplify and harden the current CLI path.

## [v0.49.0] - 2026-02-24
### Added
- Added `si vault backend` commands (`status`, `use`) and `vault.sync_backend` policy support for explicit backend sync modes.
- Added automated GitHub Release asset publishing for `si` CLI archives (`linux/amd64`, `linux/arm64`, `linux/armv7`, `darwin/amd64`, `darwin/arm64`) with generated `checksums.txt`.
- Added `si build self release-assets` to run local release-archive preflight builds using the same target matrix used by release automation.

### Changed
- Changed vault auto-backup behavior to use explicit backend policy resolution, with backward-compatible fallback from legacy auto-sync settings.
- Hardened remote vault backup pull flow with payload checksum/size verification when object metadata is available.
- Changed `si vault run` subprocess environment handling to strip inherited `GIT_*` variables.

### Security
- Hardened the remote vault client URL policy to require HTTPS for non-loopback endpoints by default.

## [v0.48.0] - 2026-02-22
### Added
- Added cloud account auth, profile sync, vault backup sync, token lifecycle, audit listing, and health diagnostics coverage.
- Added cloud-sync CI coverage and subprocess round-trip tests for profile sync and vault backup flows.
- Added cloud-sync operator documentation and settings reference coverage.
- Added native Go SSH transport support for remote deploy workflows, with companion architecture runbook documentation.

### Changed
- Updated GitHub and Cloudflare auth resolution so account credentials can be sourced directly from SI vault key references.
- Improved `si login` browser/headless behavior and clarified Safari accessibility guidance for profile-aware URL opening.

### Fixed
- Stabilized SI CI lanes by fixing empty-env headless detection and formatting gate regressions.
- Fixed `si vault` write paths to refuse updates when git index flags (`skip-worktree`/`assume-unchanged`) could hide dotenv changes.
- Fixed `si logout-all` behavior to block unintended auth-cache recovery after explicit logout.

### Security
- Hardened vault auto-backup hooks to skip uploading dotenv files containing plaintext keys, preventing accidental plaintext secret sync to remote storage.

## [v0.47.0] - 2026-02-19
### Added
- Added the historical Orbitals command surface (`si orbits`) with catalog build/validate, policy controls (including namespace wildcards), update flows, and install diagnostics/provenance reporting.
- Added SI-managed Supabase backup workflows with WAL-G/Databackup profile support, including contract/run/status/restore operations.
- Added GitHub git-credential helper and remote normalization workflows under `si github git`, plus expanded PAT OAuth guidance for multi-repo operations.
- Added first-run workspace defaults prompting/persistence and a strict vault-focused regression suite with explicit default vault-file management.
- Added `si mintlify` docs lifecycle integration and Gemini image generation support in `si gcp`.

### Changed
- Reorganized and expanded Mintlify docs structure, command references, and integration guides for complete current command coverage.
- Hardened CI/workflow behavior with docs-scope gating, workflow sanity checks, installer smoke lanes, and segmented plugin matrix runners.

### Fixed
- Fixed settings loading/ownership edge cases that produced permission-denied warnings for `~/.si/settings.toml` on host-driven executions.
- Fixed installer smoke and website-sentry workflow output handling, plus environment-dependent codex/platform test flakiness.

### Removed
- Removed internal planning/taskboard/market-research artifacts and retired related automation surfaces from tracked docs/workflows.

### Security
- Removed internal-only documentation pages and references from tracked history to reduce accidental exposure risk ahead of public repository usage.

## [v0.46.1] - 2026-02-18
### Fixed
- Fixed release version metadata by updating the `si version` output to report `v0.46.1` in built binaries.

## [v0.46.0] - 2026-02-18
### Added
- Added platform operational workflows for deploy event recording, health rollback orchestration with failure taxonomy, release bundle metadata, scrubbed metadata export/import, context vault namespace controls, logs/events live backends, operational alert routing, and operator callback acknowledge hooks.
- Added incident and automation features: unified audit event model, incident queue retention, incident schema taxonomy dedupe, event bridge collectors, codex runtime adapter, live agent command backend, remediation policy engine, scheduler self-heal locks, offline fake-codex smoke loop, and agent-run audit artifact capture.
- Added state and governance artifacts, including context state layout/init, state-classification storage policy, isolation guardrails, addon lifecycle/magic-variable merge validation, security checklist and threat model, failure-injection rollback drills, regression coverage, backup/restore policy, and context/incident operations runbooks.
- Added `si google play` direct API automation with service-account auth/context, custom app creation, listing/details/image management, release-track orchestration, metadata apply workflow, provider telemetry registration, and raw API access.
- Added `si apple appstore` direct API automation with JWT auth/context, bundle/app onboarding, localized listing updates, metadata apply workflows, provider telemetry registration, and raw App Store Connect API access.

### Changed
- Defaulted existing-session `si run` to tmux attach mode and added `--no-tmux` as the explicit opt-out for direct shell/custom command execution.
- Unified user-facing datetime rendering to GitHub-style relative dates and updated `si status`/weekly reset displays to show date-only absolute values plus relative countdowns.

### Fixed
- Fixed an alerting crash path by guarding nil-map access in operational alert dispatch.
- Fixed `tools/si` module dependency metadata drift so release builds no longer fail with `go: updates to go.mod needed`.

## [v0.45.0] - 2026-02-11
### Added
- Added `si publish` with DistributionKit catalog listing and provider-specific publish flows.
- Added direct API command families across OpenAI, AWS, GCP, WorkOS, OCI, and image providers (Unsplash, Pexels, Pixabay), including broad AWS and Bedrock coverage plus GCP IAM/API key/Gemini/Vertex AI command suites.
- Added Cloudflare Pages custom-domain CRUD support under `si cloudflare pages domain`.
- Added expanded vault workflows: `si vault decrypt` (in-place and `--stdout`), `si vault keygen`, `si vault run --shell`, and support for arbitrary env file paths.

### Changed
- Removed the top-level `si self` command surface in favor of `si build self`.
- Completed direct Go HTTP execution paths for core integrations (Cloudflare, GitHub, Google Places, YouTube, Stripe) using shared runtime/retry semantics.
- Simplified vault targeting from repo/submodule-oriented directories to a file-first model (`vault.file` default + optional `--file`) across commands, trust, help text, and docs.
- Unified provider runtime metadata/health characteristics and expanded integration guardrails and e2e coverage.

### Fixed
- Reworked Stripe to direct HTTP requests (no `stripe-go` runtime dependency), including improved retry and error normalization.
- Hardened vault parsing/format fidelity and command safety checks, including stricter header parsing, quote validation, non-interactive keyring restrictions, and clearer macOS Keychain failure diagnostics.

### Removed
- Removed vault submodule-oriented flags and initialization/status plumbing (`--vault-dir`, submodule bootstrap wiring, and `.gitmodules`-specific helpers) in favor of file-based targeting.

### Security
- Hardened CLI file/exec handling and vault path-safety boundaries to reduce traversal and unsafe invocation risk.

## [v0.44.0] - 2026-02-08
### Added
- Added `si github` command family with GitHub App-only auth, account context management, and direct REST/GraphQL bridge support.
- Added `si cloudflare` command family with token-auth context management (`auth`, `context`, `doctor`) plus raw API access.
- Added `si google places` command family with API-key auth/context flows (`auth`, `context`, `doctor`), session lifecycle helpers, and raw API access.
- Added `si google youtube` command family with API key + OAuth device-flow auth, broad resource coverage, and raw API access.
- Added `si self` commands to build/upgrade/run `si` from a repo checkout (`si self build`, `si self upgrade`, `si self run`).
- Added `tools/install-si.sh` installer for macOS and Linux, plus `tools/test-install-si.sh` for installer e2e coverage.
- Added dedicated command guides: `docs/VAULT.md`, `docs/GITHUB.md`, `docs/CLOUDFLARE.md`, `docs/GOOGLE_PLACES.md`, and `docs/GOOGLE_YOUTUBE.md`.

### Changed
- Renamed `si images` to `si image` (singular) and refreshed help/docs to match.
- Updated CLI dispatch/help and settings schema to include vault + GitHub/Cloudflare/Google integrations.

### Fixed
- Fixed GitHub release asset upload handling to use GitHub-provided `upload_url` metadata instead of hardcoded host assumptions, improving GHES/base URL compatibility.
- Fixed first-run settings creation to apply defaults on initial settings generation.
- Hardened vault initialization for submodule edge cases (HEADless clones, absent submodule config) and reduced unnecessary `git submodule update` work.
- Fixed installer cleanup trapping to avoid leaving temporary build directories behind.

## [v0.43.0] - 2026-02-07
### Added
- Added `si stripe` command family with account context, object CRUD, raw API access, reporting presets, and live-to-sandbox sync plan/apply.
- Added Stripe bridge internals for normalized request execution, pagination helpers, object registry mapping, and sync orchestration.
- Added structured Stripe JSONL observability logs at `~/.si/logs/stripe.log` (configurable via settings/env).
- Added Stripe test coverage for auth/config parsing, registry/CRUD mapping, bridge client behavior, sync planning, report logic, and output mode handling.

### Changed
- Updated CLI help and docs with Stripe multi-account `live|sandbox` workflows and sandbox-first terminology.
- Updated Stripe ticket status tracking with per-workstream completion notes.

### Fixed
- Fixed Stripe command flag parsing so positional and flag arguments can be passed in mixed order.
- Improved wrapped Stripe error rendering so full actionable details remain visible with contextual command errors.

## [v0.42.0] - 2026-02-07
### Added
- Added profile-indexed spawn guards so codex profiles cannot create multiple worker sessions.
- Added deterministic profile-session selection tests for spawn/respawn enforcement.
### Changed
- Merged `si profile` behavior into `si status`, including list/default/single-profile flows.
- Defaulted `si status` to include profile usage columns and added `--no-status` for classic output.
- Defaulted spawn/respawn profile flows to use the profile ID as the worker-session name.
- Hardened respawn profile flows to clean up legacy duplicate worker sessions for the same profile.
### Fixed
- Treated expired usage API tokens as stale profile auth instead of hard status errors.
- Fixed status/profile argument parsing so flags work both before and after positional values.

## [v0.41.0] - 2026-01-30
### Added
- Added automatic login URL opening with Safari profile support and overrides.
- Added device code clipboard copy for macOS and Linux.
### Changed
- Updated the release process guide with end-to-end steps, tagging, and GitHub release flow.
### Fixed
- Stripped ANSI escape sequences from login URLs.

## [v0.40.0] - 2026-01-27
### Added
### Changed
- Refreshed CLI help and docs to match the new image layout and flag ordering.
### Removed
### Fixed

## [v0.39.1] - 2026-01-26
### Changed
- Renamed the markdown profile command to `si persona`.

### Removed
- Removed the `si codex ...` namespace in favor of top-level commands.

## [v0.39.0] - 2026-01-26
### Added
- Introduced Codex profiles and the `si codex profile` command.
- Added profile-aware `si codex login` with host auth caching.
- Added `~/.si/settings.toml` for unified configuration and prompt theming.
- Added shell prompt rendering driven by settings without editing `.bashrc`.

### Changed
- Aligned CLI table widths using Unicode-aware display widths.
- Replaced host `.bashrc` injection with settings-driven configuration.

### Fixed
- Ensured Codex config and auth paths are created before copy operations.

## [v0.38.0] - 2026-01-26
### Added

### Changed
- Simplified layout/tooling and removed stack services.

## [v0.37.0] - 2026-01-23
### Added
- Added `si codex respawn` and `--volumes` for codex removal.

## [v0.36.0] - 2026-01-23
### Added
- Added ANSI color theming for the CLI.
- Added Colima socket detection and workspace mirroring.
- Added codex aliases, terminal titles, and config seeding on spawn.

## [v0.35.0] - 2026-01-23
### Added
- Added one-off Codex exec mode and expanded CLI help.

## [v0.34.0] - 2026-01-23
### Added

### Fixed
- Fixed image build and dependency issues.

## [v0.33.0] - 2026-01-22
### Added
- Added tmux-driven Codex status capture, ANSI report capture, and turn parsing.

### Changed
- Hardened Codex status capture flow.

## [v0.32.0] - 2026-01-22
### Added

### Fixed
- Stabilized the Codex image entrypoint and made `si` symlink-safe.

## [v0.31.0] - 2026-01-22
### Removed

## [v0.30.0] - 2025-12-31
### Added
- Added shared Postgres platform tooling.

## [v0.29.0] - 2025-12-31
### Changed
- Improved Codex status reporting and set global model defaults.

### Fixed
- Hardened Telegram status handling and probes.

## [v0.28.0] - 2025-12-29
### Added
- Added Codex account reset flow with cooldown trigger and queued re-login.

## [v0.27.0] - 2025-12-29
### Added
- Added Gatekeeper policies for secret references and expanded policy scope.

## [v0.26.0] - 2025-12-28
### Added

### Fixed

## [v0.25.0] - 2025-12-28
### Added
- Added Codex loop automation, parser, and beam activity helpers.

## [v0.24.0] - 2025-12-28
### Added
- Added SOPS+age secrets workflow and hardened agent runtime supervision.

## [v0.23.0] - 2025-12-27
### Added
- Added task complexity fields and propagated complexity to spawning and tuning.

## [v0.22.0] - 2025-12-27
### Added
- Added Codex monitor reporting for weekly usage, model, and reasoning.

## [v0.21.0] - 2025-12-26
### Added
- Added Temporal Postgres persistence configuration.

## [v0.20.0] - 2025-12-26
### Added

## [v0.19.0] - 2025-12-26
### Added
- Added monorepo docs/workspace scaffold and app bootstrap/adoption scripts.
- Added shared SvelteKit packages, deployment templates, and validation checks.

## [v0.18.0] - 2025-12-26
### Added

### Changed
- Reorganized test scripts and improved run-task TTY handling.

### Fixed
- Made app-db cleanup resilient and handled missing secrets safely.

## [v0.17.0] - 2025-12-26
### Added

### Changed

## [v0.16.0] - 2025-12-25
### Added
- Added Codex monitor account email display.

### Changed
- Improved Codex status polling and stopped defaulting Telegram parse mode.

## [v0.15.0] - 2025-12-24
### Added
- Added Codex usage monitor service and pool routing/cooldowns.
- Added status capture via local Codex PTY and monitor config updates.

## [v0.14.0] - 2025-12-24
### Added
- Added dashboard UI and ReleaseParty scaffold.

## [v0.13.0] - 2025-12-16
### Added
- Added security audit checklists, host tooling docs, and install scripts.

### Changed
- Disabled auto-enabling MCP servers and hardened MCP defaults.

## [v0.12.0] - 2025-12-16
### Added

## [v0.11.0] - 2025-12-16
### Added
- Added app lifecycle guide, bootstrap script, and per-app Postgres tooling.
- Added visual QA harness, resource limits, and capability probes.

## [v0.10.0] - 2025-12-16
### Added
- Added Pulumi scaffold/preview helper and pre-deploy checklist.
- Added interactive health endpoint and Telegram control.

## [v0.9.0] - 2025-12-16
### Added
- Added system health/QA helpers, resource/infra brokers, and metrics endpoint.
- Added Telegram command menu and button support.

## [v0.8.0] - 2025-12-16
### Added

### Changed
- Added cleanup script and maintenance guide.

## [v0.7.0] - 2025-12-16
### Added
- Added emoji/buttons support to Telegram notify and bot-driven task creation.
- Added escalation helpers and improved status formatting.

## [v0.6.0] - 2025-12-16
### Added
- Added access request workflow/helpers and hierarchy docs.
- Added Telegram token rotation helper and credentials guidance.

## [v0.5.0] - 2025-12-16
### Added
- Added sample Go service and testing harness docs.

## [v0.4.0] - 2025-12-16
### Added
- Added Telegram notifier, human tasks API, and task persistence.

### Changed
- Made critic polling configurable and hardened notifier chat handling.

## [v0.3.0] - 2025-12-16
### Added

## [v0.2.0] - 2025-12-13
### Added

### Changed

## [v0.1.0] - 2025-12-13
### Added
- Initial bootstrap scaffold.
