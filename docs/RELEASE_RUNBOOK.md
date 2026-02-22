# Release Runbook

This repo uses Git tags + GitHub Releases. Follow this order to avoid broken/partial releases.

## Preconditions

- Local worktree is clean: `git status`
- CI is green on `main`
- You have GitHub permissions to push tags and create releases

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
- Confirms archive packaging/checksum generation before publishing a GitHub Release.

## 6. Create GitHub Release

1. In GitHub UI: Releases -> "Draft a new release".
1. Choose the tag `vX.Y.Z` on `main`.
1. Title format:
   - `vX.Y.Z - <short title>`
   - The `<short title>` should be a 3-7 word summary of the headline change (not "Release" / not a sentence).
1. Body:
   - Paste the `CHANGELOG.md` section for that version.
   - Add a short "Upgrade notes" section if there are any behavior changes.
1. Publish the release.
1. After publish, wait for workflow `CLI Release Assets` to complete (it auto-builds and uploads release archives).

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
