# Release Runbook

This repo uses Git tags + GitHub Releases. Follow this order to avoid broken/partial releases.

## Preconditions

- Local worktree is clean: `git status`
- CI is green on `main`
- You have GitHub permissions to push tags and create releases
- Repo secrets are configured for ReleaseMind automation:
  - `RELEASEMIND_API_BASE_URL`
  - `RELEASEMIND_AUTOMATION_TOKEN`

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

## 6. Run automated release runbook

After the tag is pushed, workflow `.github/workflows/releasemind-release.yml`
auto-runs on tag push and performs:

1. Ensures a draft release exists for `vX.Y.Z`.
1. Asks ReleaseMind to prepare release notes.
1. Publishes the release after the draft is ready.

Manual trigger variant:

```bash
gh workflow run releasemind-release.yml -f tag=vX.Y.Z -f publish=true
```

Manual fallback (if automation is disabled):

1. In GitHub UI: Releases -> "Draft a new release".
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
  - `gh run list --workflow "ReleaseMind Release Runbook" --limit 1`
  - `gh run list --workflow "CLI Release Assets" --limit 1`
  - `gh release view vX.Y.Z --json assets --jq '.assets[].name'`
  - Confirm these files exist:
    - `si_<version>_linux_amd64.tar.gz`
    - `si_<version>_linux_arm64.tar.gz`
    - `si_<version>_linux_armv7.tar.gz`
    - `si_<version>_darwin_amd64.tar.gz`
    - `si_<version>_darwin_arm64.tar.gz`
    - `checksums.txt`
