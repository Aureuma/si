# Release Runbook

This repo uses Git tags + GitHub Releases. Follow this order to avoid broken/partial releases.

## Preconditions

- Local worktree is clean: `git status`
- CI is green on `main`
- You have GitHub permissions to push tags and create releases

## 1. Decide Version

- Pick next semver tag, e.g. `v0.42.0`.
- Keep `v0.x.y` consistent with prior tags in this repo.

## 2. Update Changelog

1. Edit `CHANGELOG.md`.
1. Add a new top section for the version/date, e.g.:
   - `## [v0.42.0] - 2026-02-09`
1. Add bullets grouped by area (Dyad, CLI, Image, Docs, Vault, etc.).
1. Ensure the items are user-facing (what changed) and include important migration notes.
1. Update `tools/si/version.go`:
   - `const siVersion = "v0.42.0"`

## 3. Commit

1. Commit release prep changes:
   - `git add CHANGELOG.md tools/si/version.go`
   - `git commit -m "release: v0.42.0"`

## 4. Tag

1. Create an annotated tag:
   - `git tag -a v0.42.0 -m "v0.42.0"`

## 5. Push

1. Push commit(s):
   - `git push origin main`
1. Push tag:
   - `git push origin v0.42.0`

## 6. Create GitHub Release

1. In GitHub UI: Releases -> "Draft a new release".
1. Choose the tag `v0.42.0` on `main`.
1. Title format:
   - `v0.42.0 - <short title>`
   - The `<short title>` should be a 3-7 word summary of the headline change (not "Release" / not a sentence).
1. Body:
   - Paste the `CHANGELOG.md` section for that version.
   - Add a short "Upgrade notes" section if there are any behavior changes.
1. Publish the release.

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
