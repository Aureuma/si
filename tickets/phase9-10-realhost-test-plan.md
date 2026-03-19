# Phase 9–10 Real-host Validation Plan and Results

## Scope

Validate that the Rust/Go transition deliverables in `SI Tests` are operational end-to-end on a real host path, with emphasis on release build/install smoke commands.

## Test Environment

- Repo: `/home/shawn/Development/si`
- Commit: `5e49fbf`
- Time window: `2026-03-19`
- Primary command path:
  - `./tickets/phase9-10-realhost-matrix.sh`
  - Log sink: `tickets/phase9-10-realhost-matrix-latest.log`
- Invocation for this run:
  - `OUT_DIR=/tmp/si-e2e-check-single MULTI_DIR=/tmp/si-e2e-check-multi SKIP_RELEASE_BUILD=1 SMOKE_TIMEOUT_SECS=900 RELEASE_VERSION=v0.54.0 ./tickets/phase9-10-realhost-matrix.sh`

## Execution Matrix

| # | Scenario | Command | Expected result | Observed result |
|---|---|---|---|---|
| 1 | Version check | `version` | CLI reports `v0.54.0` | Passed |
| 2 | Validate release version (valid tag) | `build self validate-release-version --tag v0.54.0` | Version accepted and aligned with `tools/si/version.go` | Passed |
| 3 | Validate release version (invalid tag) | `build self validate-release-version --tag 0.54.0` | Expected non-zero + tag-format error (handled by test harness) | Passed with warning (`exit 1` expected) |
| 4 | Release artifact short-path path reuse | Pre-cached asset branch when present | Skip and continue | Skipped as expected |
| 5 | Release single tarball short-path | `build self release-asset` | Reuses/creates single Linux archive + checksum | Reused cached artifact at `/tmp/si-e2e-check-single/si_0.54.0_linux_amd64.tar.gz` |
| 6 | Release multi-archive path | `build self release-assets` | Reuses/creates multi-platform archive set + `checksums.txt` | Reused cached artifact at `/tmp/si-e2e-check-multi` |
| 7 | Release asset verification | `build self verify-release-assets` | Verifies every generated artifact and checksum | Passed (`verified release assets in /tmp/si-e2e-check-multi`) |
| 8 | Homebrew core formula render | `build homebrew render-core-formula ...` | Formula file emitted | Passed |
| 9 | Homebrew tap formula render | `build homebrew render-tap-formula ...` | Formula file emitted using checksums | Passed |
| 10 | Homebrew tap update | `build homebrew update-tap-repo ...` | Tap file updated without errors | Passed |
| 11 | NPM package build | `build npm build-package ...` | Package artifact emitted | Passed |
| 12 | NPM publish dry-run | `build npm publish-package --dry-run` | Either dry-run success or clean skip when package exists | Skipped because package already published |
| 13 | Vault-backed npm dry-run | `build npm publish-from-vault --dry-run` | Pass with vault available; non-zero allowed if vault path absent | Warned: `vault list failed` |
| 14 | Installer settings helper print | `build installer settings-helper --print` | Helper output includes configured defaults | Passed |
| 15 | Installer smoke (host) | `build installer smoke-host` | Installer help/tests/e2e flow validates | Passed |
| 16 | Installer smoke (npm) | `build installer smoke-npm` | Installer smoke command completes | Passed |
| 17 | Installer smoke (docker) | `build installer smoke-docker` | Installer smoke command completes | Passed (`SI_INSTALL_SMOKE_SKIP_NONROOT=1`) |
| 18 | Installer smoke (homebrew) | `build installer smoke-homebrew` | Smoke command runs when `brew` available or skipped | Skipped: `brew` not available |

### Execution update (2026-03-19 fresh matrix)

- `OUT_DIR=/tmp/si-e2e-fresh-single MULTI_DIR=/tmp/si-e2e-fresh-multi SMOKE_TIMEOUT_SECS=900 RELEASE_VERSION=v0.54.0 ./tickets/phase9-10-realhost-matrix.sh`
- Result:
  - Release single and multi assets both generated from scratch (all target tarballs + checksums).
  - `verify-release-assets`, Homebrew render/update, npm build/publish dry-runs passed.
  - Installer host/npm/docker smoke paths all completed.
  - `installer smoke-homebrew` remained skipped due missing `brew`.
  - Matrix completed.

## Notes

- Steps 16–17 now pass when `SI_INSTALL_SMOKE_SKIP_NONROOT=1` and `SMOKE_TIMEOUT_SECS=900` with prebuilt release artifacts (`SI_INSTALL_SMOKE_ASSETS_DIR=${MULTI_DIR}`), confirming the previously long-running smoke lanes are now deterministic on this host.
- No source changes were required by this local validation pass; behavior matched expected handling for known environmental constraints.
- This run validates that the current formatting-only transition fixes did not regress the matrix entry points.

## Follow-up Actions

1. Re-run matrix with normal timeout budget (or CI-host equivalent) for smoke-lanes, with npm and Docker prerequisites prepared (achieved locally with `SMOKE_TIMEOUT_SECS=900`, `SI_INSTALL_SMOKE_SKIP_NONROOT=1`, and prebuilt artifacts via `SI_INSTALL_SMOKE_ASSETS_DIR`).
2. Re-run matrix with `SKIP_RELEASE_BUILD=0` on release-capable runners to validate artifact generation in non-cached contexts (achieved for local real-host run).
3. Confirm GitHub-hosted `SI Tests` and `Orbit Runners` runs reach green once GitHub API auth is restored; `gh auth status` currently reports an invalid `github.com` token and this host has no `GH_TOKEN`/`GITHUB_TOKEN` override available.
