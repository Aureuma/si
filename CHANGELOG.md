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

## [v0.49.0] - 2026-02-24
### Added
- Added `si vault backend` commands (`status`, `use`) and `vault.sync_backend` policy support for explicit `git`, `dual`, and `sun` vault sync modes.
- Added automated GitHub Release asset publishing for `si` CLI archives (`linux/amd64`, `linux/arm64`, `linux/armv7`, `darwin/amd64`, `darwin/arm64`) with generated `checksums.txt`.
- Added `si build self release-assets` to run local release-archive preflight builds using the same target matrix used by release automation.

### Changed
- Changed vault auto-backup behavior to use explicit backend policy resolution, with backward-compatible fallback from legacy `sun.auto_sync`.
- Hardened Sun vault backup pull flow with payload checksum/size verification when object metadata is available.
- Changed `si vault run` subprocess environment handling to strip inherited `GIT_*` variables.

### Security
- Hardened Sun client URL policy to require HTTPS for non-loopback endpoints by default (override with `SI_SUN_ALLOW_INSECURE_HTTP=1` only when intentional).

## [v0.48.0] - 2026-02-22
### Added
- Added the `si sun` cloud command surface for account auth, codex profile sync, vault backup sync, token lifecycle, audit listing, and health diagnostics.
- Added Sun-focused CI coverage and subprocess round-trip tests for profile sync and vault backup flows.
- Added Sun cloud-sync operator documentation and settings reference coverage (`[sun]`).
- Added native Go SSH transport support for SI PaaS remote deploy workflows, with companion architecture runbook documentation.

### Changed
- Updated GitHub and Cloudflare auth resolution so account credentials can be sourced directly from SI vault key references.
- Improved `si login` browser/headless behavior and clarified Safari accessibility guidance for profile-aware URL opening.
- Simplified `si build image` mode selection while preserving native `docker buildx` progress output behavior.

### Fixed
- Stabilized SI CI lanes (`SI Tests` and Sun-focused workflow checks) by fixing empty-env headless detection and formatting gate regressions.
- Fixed `si vault` write paths to refuse updates when git index flags (`skip-worktree`/`assume-unchanged`) could hide dotenv changes.
- Fixed `si logout-all` behavior to block unintended auth-cache recovery after explicit logout.

### Security
- Hardened Sun vault auto-backup hooks to skip uploading dotenv files containing plaintext keys, preventing accidental plaintext secret sync to cloud storage.

## [v0.47.0] - 2026-02-19
### Added
- Added a full plugin marketplace command surface (`si plugins`) with catalog build/validate, policy controls (including namespace wildcards), update flows, and install diagnostics/provenance reporting.
- Added `si browser` Docker runtime integration for Playwright MCP and wired browser MCP endpoints into spawned codex and dyad containers.
- Added SI-managed Supabase backup workflows with WAL-G/Databackup profile support under `si paas backup`, including contract/run/status/restore operations.
- Added GitHub git-credential helper and remote normalization workflows under `si github git`, plus expanded PAT OAuth guidance for multi-repo operations.
- Added first-run workspace defaults prompting/persistence and a strict vault-focused regression suite with explicit default vault-file management.
- Added `si mintlify` docs lifecycle integration and Gemini image generation support in `si gcp`.

### Changed
- Reorganized and expanded Mintlify docs structure, command references, and integration guides for complete current command coverage.
- Hardened CI/workflow behavior with docs-scope gating, workflow sanity checks, installer smoke lanes, and segmented plugin matrix runners.
- Hardened installer and image build flows for docker root/non-root environments and BuildKit/buildx fallback behavior.

### Fixed
- Fixed settings loading/ownership edge cases that produced permission-denied warnings for `~/.si/settings.toml` on host-driven executions.
- Fixed mixed boolean flag reordering behavior and dyad `--skip-auth` boolean parsing edge cases.
- Fixed installer smoke and website-sentry workflow output handling, plus environment-dependent codex/PaaS test flakiness.
- Fixed host/container tooling parity by mounting host Docker config + SI Go toolchain into codex/dyad containers and resolving preflight/analyze Go without host PATH dependence.

### Removed
- Removed internal planning/taskboard/market-research artifacts and retired related automation surfaces from tracked docs/workflows.

### Security
- Removed internal-only documentation pages and references from tracked history to reduce accidental exposure risk ahead of public repository usage.

## [v0.46.1] - 2026-02-18
### Fixed
- Fixed release version metadata by updating the `si version` output to report `v0.46.1` in built binaries/images.

## [v0.46.0] - 2026-02-18
### Added
- Added the initial `si paas` platform management surface, including target storage/bootstrap, connectivity checks, compatibility preflight checks, Traefik ingress secret helpers, deploy strategy fan-out, webhook ingest/mapping, and compose-only blue/green cutover policy controls.
- Added PaaS operational workflows for deploy event recording, health rollback orchestration with failure taxonomy, release bundle metadata, scrubbed metadata export/import, context vault namespace controls, logs/events live backends, operational alert routing (including Telegram), and operator callback acknowledge hooks.
- Added PaaS incident and automation features: unified audit event model, incident queue retention, incident schema taxonomy dedupe, event bridge collectors, codex runtime adapter, live agent command backend, remediation policy engine, scheduler self-heal locks, offline fake-codex smoke loop, and agent-run audit artifact capture.
- Added PaaS state and governance artifacts, including context state layout/init, state-classification storage policy, isolation guardrails, addon lifecycle/magic-variable merge validation, security checklist and threat model, failure-injection rollback drills, regression coverage, backup/restore policy, and context/incident operations runbooks.
- Added `si google play` direct API automation with service-account auth/context, custom app creation, listing/details/image management, release-track orchestration, metadata apply workflow, provider telemetry registration, and raw API access.
- Added `si apple appstore` direct API automation with JWT auth/context, bundle/app onboarding, localized listing updates, metadata apply workflows, provider telemetry registration, and raw App Store Connect API access.

### Changed
- Defaulted existing-container `si run` to tmux attach mode and added `--no-tmux` as the explicit opt-out for direct shell/custom command execution.
- Unified user-facing datetime rendering to GitHub-style relative dates and updated `si status`/weekly reset displays to show date-only absolute values plus relative countdowns.
- Hardened Docker runtime compatibility for Colima-based macOS setups (including profile/context socket detection) and made `si build image` gracefully skip build secrets when `docker buildx` is unavailable.

### Fixed
- Fixed a PaaS alerting crash path by guarding nil-map access in operational alert dispatch.
- Fixed `tools/si` module dependency metadata drift so image builds no longer fail with `go: updates to go.mod needed`.
- Fixed `si build image` to disable BuildKit (`DOCKER_BUILDKIT=0`) when `docker buildx` is missing or broken, preventing BuildKit hard-fail errors on Colima-only hosts.

## [v0.45.0] - 2026-02-11
### Added
- Added `si publish` with DistributionKit catalog listing and provider-specific publish flows.
- Added direct API command families across OpenAI, AWS, GCP, WorkOS, OCI, and image providers (Unsplash, Pexels, Pixabay), including broad AWS and Bedrock coverage plus GCP IAM/API key/Gemini/Vertex AI command suites.
- Added Cloudflare Pages custom-domain CRUD support under `si cloudflare pages domain`.
- Added expanded vault workflows: `si vault decrypt` (in-place and `--stdout`), `si vault keygen`, `si vault run --shell`, and support for arbitrary env file paths.

### Changed
- Moved build operations under `si build` (`si build image`, `si build self`, `si build self upgrade`, `si build self run`).
- Removed the top-level `si self` command surface in favor of `si build self`.
- Completed direct Go HTTP execution paths for core integrations (Cloudflare, GitHub, Google Places, YouTube, Stripe) using shared runtime/retry semantics.
- Simplified vault targeting from repo/submodule-oriented directories to a file-first model (`vault.file` default + optional `--file`) across commands, trust, help text, and docs.
- Unified provider runtime metadata/health characteristics and expanded integration guardrails and e2e coverage.

### Fixed
- Reworked Stripe to direct HTTP requests (no `stripe-go` runtime dependency), including improved retry and error normalization.
- Hardened vault parsing/format fidelity and command safety checks, including stricter header parsing, quote validation, non-interactive keyring restrictions, and clearer macOS Keychain failure diagnostics.
- Fixed dyad/codex workspace mount and loop handoff edge cases, including prompt readiness and recovery controls.

### Removed
- Removed vault submodule-oriented flags and initialization/status plumbing (`--vault-dir`, submodule bootstrap wiring, and `.gitmodules`-specific helpers) in favor of file-based targeting.

### Security
- Hardened CLI file/exec handling and vault path-safety boundaries to reduce traversal and unsafe invocation risk.

## [v0.44.0] - 2026-02-08
### Added
- Added `si vault` git-based encrypted credentials management (age recipients, TOFU trust store, formatter, audit log, and Docker-friendly runtime injection).
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
- Added profile-indexed spawn guards so codex profiles cannot create multiple containers.
- Added deterministic profile-container selection tests for spawn/respawn enforcement.
### Changed
- Merged `si profile` behavior into `si status`, including list/default/single-profile flows.
- Defaulted `si status` to include profile usage columns and added `--no-status` for classic output.
- Defaulted spawn/respawn profile flows to use the profile ID as the container name.
- Hardened respawn profile flows to clean up legacy duplicate containers for the same profile.
### Fixed
- Treated expired usage API tokens as stale profile auth instead of hard status errors.
- Fixed status/profile argument parsing so flags work both before and after positional values.

## [v0.41.0] - 2026-01-30
### Added
- Added automatic login URL opening with Safari profile support and overrides.
- Added device code clipboard copy for macOS and Linux.
- Added docker socket mount toggles for codex and dyad spawns, including one-off exec.
- Added `codex.docker_socket` and `dyad.docker_socket` settings defaults.
### Changed
- Updated the release process guide with end-to-end steps, tagging, and GitHub release flow.
### Fixed
- Stripped ANSI escape sequences from login URLs.

## [v0.40.0] - 2026-01-27
### Added
- Added the unified `aureuma/si:local` image build for codex and dyad runtimes.
### Changed
- Defaulted codex and dyad images to `aureuma/si:local`.
- Updated dyad runtime entrypoints and HOME/CODEX_HOME defaults for the unified image.
- Refreshed CLI help and docs to match the new image layout and flag ordering.
### Removed
- Removed the separate base, codex, actor, and critic Docker image definitions.
### Fixed
- Corrected dyad exec and copy-login usage guidance for flag ordering.

## [v0.39.1] - 2026-01-26
### Changed
- Promoted Codex container commands to top-level (for example `si run`, alias `si exec`).
- Renamed the markdown profile command to `si persona`.

### Removed
- Removed the `si codex ...` namespace in favor of top-level commands.

## [v0.39.0] - 2026-01-26
### Added
- Introduced Codex profiles and the `si codex profile` command.
- Added profile-aware `si codex login` with host auth caching.
- Added container file read support for auth caching.
- Added `~/.si/settings.toml` for unified configuration and prompt theming.
- Added shell prompt rendering driven by settings without editing `.bashrc`.

### Changed
- Defaulted workspace mounts to the current directory for codex spawn/respawn and dyad spawn when `--workspace` is omitted.
- Aligned CLI table widths using Unicode-aware display widths.
- Replaced host `.bashrc` injection with settings-driven configuration.

### Fixed
- Ensured Codex config and auth paths are created before copy operations.

## [v0.38.0] - 2026-01-26
### Added
- Added a base image and streamlined Codex containers.

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
- Added dyad Codex tooling and updated guidance.

### Fixed
- Hardened critic Codex loop and dyad runtime behavior.
- Fixed image build and dependency issues.

## [v0.33.0] - 2026-01-22
### Added
- Added tmux-driven Codex status capture, ANSI report capture, and turn parsing.

### Changed
- Hardened Codex status capture flow.

## [v0.32.0] - 2026-01-22
### Added
- Added standalone `si codex` container workflow.

### Fixed
- Stabilized the Codex image entrypoint and made `si` symlink-safe.

## [v0.31.0] - 2026-01-22
### Removed
- Removed Temporal and JS/Svelte stacks and consolidated Docker tooling.

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
- Added credentials MCP service/registry and routing secrets via credentials dyad.
- Added Gatekeeper policies for secret references and expanded policy scope.

## [v0.26.0] - 2025-12-28
### Added
- Added k3s image build/import helper and ReleaseParty backend Dockerfile.

### Fixed
- Fixed dyad bootstrap RBAC/task encoding and set the SOPS age recipient.

## [v0.25.0] - 2025-12-28
### Added
- Added Codex loop automation, parser, and beam activity helpers.
- Added Temporal dyad bootstrap beam and restored login script permissions.

## [v0.24.0] - 2025-12-28
### Added
- Added SOPS+age secrets workflow and tini in agent containers.

## [v0.23.0] - 2025-12-27
### Added
- Added task complexity fields and propagated complexity to spawning and tuning.

## [v0.22.0] - 2025-12-27
### Added
- Added Kubernetes-aware test scripts and secret-sourced Telegram chat IDs.
- Added Codex monitor reporting for weekly usage, model, and reasoning.

## [v0.21.0] - 2025-12-26
### Added
- Added Temporal-backed manager state/worker and Kubernetes scaffolding.
- Added Kubernetes manifests, refactors, and build/test updates.
- Added Temporal Postgres persistence configuration.

## [v0.20.0] - 2025-12-26
### Added
- Added dyad roster config with assignment policy enforcement and tests.

## [v0.19.0] - 2025-12-26
### Added
- Added monorepo docs/workspace scaffold and app bootstrap/adoption scripts.
- Added shared SvelteKit packages, deployment templates, and validation checks.

## [v0.18.0] - 2025-12-26
### Added
- Added Go test runner, test layout docs, and dyad comm checks.

### Changed
- Reorganized test scripts and improved run-task TTY handling.

### Fixed
- Made app-db cleanup resilient and handled missing secrets safely.

## [v0.17.0] - 2025-12-26
### Added
- Added Swarm stack/bootstrap helpers and updated docs/configs.

### Changed
- Resolved Swarm service targets and archived legacy compose.

## [v0.16.0] - 2025-12-25
### Added
- Added dyad registry entries with enforcement and documentation.
- Added Codex monitor account email display.

### Changed
- Improved Codex status polling and stopped defaulting Telegram parse mode.

## [v0.15.0] - 2025-12-24
### Added
- Added Codex usage monitor service and pool routing/cooldowns.
- Added status capture via local Codex PTY and monitor config updates.

## [v0.14.0] - 2025-12-24
### Added
- Added dyad task board, beams, codex loop driver, and router/PM agents.
- Added dashboard UI and ReleaseParty scaffold with compose services.

## [v0.13.0] - 2025-12-16
### Added
- Added security audit checklists, host tooling docs, and install scripts.

### Changed
- Disabled auto-enabling MCP servers and hardened MCP image defaults.

## [v0.12.0] - 2025-12-16
### Added
- Integrated Docker MCP Gateway and Codex MCP config helper.
- Added dyad profile contexts, capability office guidance, and comms bridge.

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
- Added web team dyads, delivery flow, and review cron helper.

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
- Added dyad departments/list helper and feedback endpoint.
- Added sample Go service and testing harness docs.

## [v0.4.0] - 2025-12-16
### Added
- Added Telegram notifier, human tasks API, and task persistence.

### Changed
- Made critic polling configurable and hardened notifier chat handling.

## [v0.3.0] - 2025-12-16
### Added
- Added dyad actor/critic containers with manager and dynamic spawn scripts.

## [v0.2.0] - 2025-12-13
### Added
- Added coder agent container and compose setup.

### Changed
- Dropped compose version field to silence warnings.

## [v0.1.0] - 2025-12-13
### Added
- Initial bootstrap scaffold.
