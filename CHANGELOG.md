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
- Merged `si image build` and `si self build` under `si build`:
  - `si build image`
  - `si build self` (plus `si build self upgrade` / `si build self run`)

## [v0.44.0] - 2026-02-08
### Added
- Added `si vault` git-based encrypted credentials management (age recipients, TOFU trust store, formatter, audit log, and Docker-friendly runtime injection).
- Added `si github` command family with GitHub App-only auth, account context management, and direct REST/GraphQL bridge support.
- Added `si cloudflare` command family with token-auth context management (`auth`, `context`, `doctor`) plus raw API access.
- Added `si google places` command family with API-key auth/context flows (`auth`, `context`, `doctor`), session lifecycle helpers, and raw API access.
- Added `si google youtube` command family with API key + OAuth device-flow auth, broad resource coverage, and raw API access.
- Added `si build self` commands to build/upgrade/run `si` from a repo checkout (`si build self`, `si build self upgrade`, `si build self run`).
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
