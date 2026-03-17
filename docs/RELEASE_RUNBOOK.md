# Release Runbook

This repo uses Git tags + GitHub Releases. Follow this order to avoid broken/partial releases.

## Preconditions

- Local worktree is clean: `git status`
- CI is green on `main`
- You have GitHub permissions to push tags and create releases
- Optional distribution secrets (for full npm + Homebrew automation):
  - `NPM_TOKEN`
  - `HOMEBREW_TAP_PUSH_TOKEN`
  - `GH_PAT_AUREUMA` can act as fallback for the Homebrew tap update when it has push access to `Aureuma/homebrew-si`

## 1. Decide Version

- Pick next semver tag, e.g. `vX.Y.Z`.
- Keep `v0.x.y` consistent with prior tags in this repo.

## 2. Update Changelog

1. Edit `CHANGELOG.md`.
1. Add a new top section for the version/date, e.g.:
   - `## [vX.Y.Z] - YYYY-MM-DD`
1. Add bullets grouped by area (Dyad, CLI, Image, Docs, Vault, etc.).
1. Ensure the items are user-facing (what changed) and include important migration notes.
1. Update `tools/si/version.go`:
   - `const siVersion = "vX.Y.Z"`

## 3. Commit

1. Commit release prep changes:
   - `git add CHANGELOG.md tools/si/version.go`
   - `git commit -m "release: vX.Y.Z"`

## 4. Tag

1. Create an annotated tag:
   - `git tag -a vX.Y.Z -m "vX.Y.Z"`

## 5. Push

1. Push commit(s):
   - `git push origin main`
1. Push tag:
   - `git push origin vX.Y.Z`

## 5.5 Local release-assets preflight

- Run:
  - `si build self release-assets --version vX.Y.Z --out-dir .artifacts/release-preflight`
  - `tools/release/verify-cli-release-assets.sh --version vX.Y.Z --out-dir .artifacts/release-preflight`
- Confirms archive packaging/checksum generation before publishing a GitHub Release.

## 6. Publish GitHub release

1. In GitHub UI: Releases -> "Draft a new release" (or use `gh release create`).
1. Choose the tag `vX.Y.Z` on `main`.
1. Title format: `vX.Y.Z - <short title>`.
1. Body: paste the `CHANGELOG.md` section and add upgrade notes.
1. Publish.

## 7. Post-release Checks

- Local version:
  - `si version`
- Image version:
  - `si build image`
  - `docker run --rm aureuma/si:local si version`
- Dyad smoke:
  - `HOME=/home/<user> si dyad spawn <name> --skip-auth`
  - `HOME=/home/<user> si dyad status <name>`
  - `HOME=/home/<user> si dyad remove <name>`
- Release assets:
  - `gh run list --workflow "CLI Release Assets" --limit 1`
  - `gh release view vX.Y.Z --json assets --jq '.assets[].name'`
  - Confirm these files exist:
    - `si_<version>_linux_amd64.tar.gz`
    - `si_<version>_linux_arm64.tar.gz`
    - `si_<version>_linux_armv7.tar.gz`
    - `si_<version>_darwin_amd64.tar.gz`
    - `si_<version>_darwin_arm64.tar.gz`
    - `checksums.txt`
- npm package:
  - `npm view @aureuma/si version`
  - Expect returned version to match `X.Y.Z`.
  - `npm install --global --prefix "$RUNNER_TEMP/si-npm-verify" @aureuma/si@X.Y.Z`
  - `SI_NPM_RELEASE_BASE_URL="https://github.com/Aureuma/si/releases/download/vX.Y.Z" "$RUNNER_TEMP/si-npm-verify/bin/si" version`
- npm publish using SI vault-managed token:
  - `tools/release/npm/publish-npm-from-vault.sh -- --version vX.Y.Z`
  - default token key: `NPM_GAT_AUREUMA_VANGUARDA`
- Homebrew tap:
  - `curl -fsSL https://raw.githubusercontent.com/Aureuma/homebrew-si/main/Formula/si.rb | grep 'version \"'`
  - Formula version should match `X.Y.Z`.
  - local smoke: `./tools/test-install-si-homebrew.sh`

Workflow `.github/workflows/cli-release-assets.yml` now performs a final
distribution verification job that checks:
- locally built release archives pass Rust-owned archive/checksum/content verification before upload
- required GitHub release assets are present
- npm package visibility/version plus installed-launcher verification against the published release assets (when `NPM_TOKEN` is configured)
- Homebrew tap version sync (when `HOMEBREW_TAP_PUSH_TOKEN` or fallback `GH_PAT_AUREUMA` is configured)
- and a separate macOS Homebrew smoke job exercises `./tools/test-install-si-homebrew.sh` on a brew-capable runner before the final gate
