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
1. Add bullets grouped by area (CLI, Image, Docs, Vault, Providers, etc.).
1. Ensure the items are user-facing (what changed) and include important migration notes.
1. Update root `Cargo.toml`:
   - `workspace.package.version = "X.Y.Z"`

## 3. Commit

1. Commit release prep changes:
   - `git add CHANGELOG.md Cargo.toml`
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
  - `./.artifacts/cargo-target/release/si-rs build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight`
  - `./.artifacts/cargo-target/release/si-rs build self verify --version vX.Y.Z --out-dir .artifacts/release-preflight`
- Confirms archive packaging/checksum generation before publishing a GitHub Release.

## 6. Publish GitHub release

1. Preferred SI CLI path:
   - `si orbit github release create Aureuma/si --tag vX.Y.Z --title "vX.Y.Z - <short title>" --notes-file release-notes.md --draft`
1. If the remote tag does not exist yet, create the release with an explicit target:
   - `si orbit github release create Aureuma/si --tag vX.Y.Z --title "vX.Y.Z - <short title>" --notes-file release-notes.md --target "$(git rev-parse HEAD)" --draft`
1. Confirm the remote tag exists:
   - `git ls-remote --tags origin`
1. Review the draft release in GitHub UI and publish.

Notes:

- SI now creates the git tag ref first when `--target` is provided and the requested tag is missing on the remote.
- If the tag is missing and `--target` is omitted, the command fails instead of creating a malformed release flow.
- For draft releases, GitHub may still show an `untagged-...` draft URL even when `tag_name` and the remote git ref are correct. Publish-time resolution is GitHub-side.

## 7. Post-release Checks

- Local version:
  - `si version`
- Image version:
  - `si build image`
  - `docker run --rm aureuma/si:local si version`
- Codex smoke:
  - `HOME=/home/<user> si codex spawn --profile <profile> --workspace "$PWD"`
  - `HOME=/home/<user> si codex list`
  - `HOME=/home/<user> si codex remove --profile <profile>`
- Viva compatibility smoke when the change touches `si viva`, Viva settings, or shared orchestration/config paths:
  - `si viva config show --format json`
  - `si viva config set --repo /home/<user>/Development/viva --build true`
  - `si viva -- version`
  - `si viva -- doctor`
  - confirm `/home/<user>/Development/viva/.github/workflows/ci.yml` and `/home/<user>/Development/viva/.github/workflows/release.yml` still match the current SI release discipline
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
  - `si build npm vault --version vX.Y.Z`
  - default token key: `NPM_GAT_AUREUMA_VANGUARDA`
- Homebrew tap:
  - `curl -fsSL https://raw.githubusercontent.com/Aureuma/homebrew-si/main/Formula/si.rb | grep 'version \"'`
  - Formula version should match `X.Y.Z`.
  - local smoke: `si build installer smoke-homebrew`

Workflow `.github/workflows/cli-release-assets.yml` now performs a final
distribution verification job that checks:
- locally built release archives pass Rust-owned archive/checksum/content verification before upload
- required GitHub release assets are present
- npm package visibility/version plus installed-launcher verification against the published release assets (when `NPM_TOKEN` is configured)
- Homebrew tap version sync (when `HOMEBREW_TAP_PUSH_TOKEN` or fallback `GH_PAT_AUREUMA` is configured)
- and a separate macOS Homebrew smoke job exercises `si build installer smoke-homebrew` on a brew-capable runner before the final gate
