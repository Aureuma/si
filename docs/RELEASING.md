# Releasing and Changelog Guide

This project follows Semantic Versioning and keeps a human-focused changelog.

## Versioning Rules
- Use SemVer: MAJOR.MINOR.PATCH (tag format: `vX.Y.Z`).
- Breaking changes:
  - Pre-1.0: bump MINOR (0.y.0).
  - 1.0+: bump MAJOR.
- Features: bump MINOR.
- Fixes/docs-only releases: bump PATCH.

## Changelog Format (Predefined)
Use this structure for each release entry:

```
## [vX.Y.Z] - YYYY-MM-DD
### Added
- ...
### Changed
- ...
### Fixed
- ...
### Removed
- ...
### Security
- ...
```

Guidelines:
- Newest first.
- Use only the sections that apply.
- Short, user-facing bullets in past tense.
- Dates are UTC in YYYY-MM-DD.

## Release Process (Best-Practice Sequence)
This sequence follows typical maintainer workflows: verify state, prepare release notes, update versions, tag, push, then publish a GitHub Release.

### 0) Pre-flight checks (clean repo, sync tags)
```
git status -sb
git fetch --tags origin
git switch main
git pull --ff-only
```
- Ensure the working tree is clean and the branch is up to date.
- Make sure you have all remote tags locally before picking the next version.

### 1) Determine the next version + release name
- Decide `vX.Y.Z` using the SemVer rules above.
- Choose a short suggested name for the release title (e.g., “Safari Login Flow”).
- Title format for GitHub Release: `vX.Y.Z - Suggested Name`.

### 2) Draft the changelog entry (authoritative notes)
1. Add a new entry to the top of `CHANGELOG.md` with today’s UTC date.
2. Summarize user-facing changes in 2–6 bullets.
3. Keep language past tense and user-focused.

### 3) Bump version strings in code
Update any version constants used by the CLI or status endpoints (currently):
- `tools/si/util.go` (`siVersion`)
- `tools/si/codex_status.go` (`clientInfo.version`)

### 4) Verify and commit the release prep
```
./tools/test.sh
./si analyze --module tools/si
git add CHANGELOG.md tools/si/util.go tools/si/codex_status.go
git commit -m "Bump version to vX.Y.Z"
```
- Keep release prep changes in a dedicated commit.

### 5) Create an annotated tag for the release commit
```
git tag -a vX.Y.Z -m "vX.Y.Z"
```
- Annotated tags are recommended for releases and are the best practice (they store metadata like tagger, date, and message).

### 6) Push commits and the new tag
```
git push
git push origin vX.Y.Z
```
- Push the tag explicitly to avoid conflicts with existing tags.
- `gh release create` can auto-create a tag if it doesn't exist; using `--verify-tag` ensures you only release a tag you already created and pushed.
- If you let `gh release create` auto-create a tag, fetch tags afterward to sync locally: `git fetch --tags origin`.
- If you do let it auto-create a tag, use `--target <branch|sha>` to pin the release to the desired commit.

### 7) Prepare release notes (from the changelog or commits)
If you need a quick summary of commits since the last release:
```
git log --oneline "$(git describe --tags --abbrev=0)..HEAD"
```
GitHub Releases should include a clear, hand-written release note (not just a verbatim copy of `CHANGELOG.md`). Use the changelog and commit history as inputs, then write a concise, user-facing summary of what changed since the last published GitHub Release.
Option A: Use the changelog entry as the release body.
1. Extract the new `vX.Y.Z` section into `release-notes.md` (manual or script).
2. Create the release with a title and notes file:
```
gh release create vX.Y.Z \\
  --title "vX.Y.Z - Suggested Name" \\
  --notes-file release-notes.md \\
  --verify-tag
```
Option B: Use the annotated tag message as the release body.
```
gh release create vX.Y.Z \\
  --title "vX.Y.Z - Suggested Name" \\
  --notes-from-tag \\
  --verify-tag
```
Notes:
- `--notes-from-tag` uses the annotated tag message when present; otherwise it falls back to the commit message.
Option C: Use GitHub auto-generated release notes and prepend highlights.
```
gh release create vX.Y.Z \\
  --title "vX.Y.Z - Suggested Name" \\
  --generate-notes \\
  --notes-start-tag vA.B.C \\
  --notes "Highlights:\\n- ...\\n- ..." \\
  --verify-tag
```
Notes:
- `--verify-tag` aborts if the tag doesn’t already exist on the remote.
- `--generate-notes` uses GitHub’s Release Notes API and can be combined with `--notes`.
- Add `--notes-start-tag vA.B.C` to define the start tag for generated notes when needed.
- Add `--fail-on-no-commits` if you want to prevent duplicate releases when there are no new commits.

### 8) Verify the published release
```
gh release view vX.Y.Z --web
```
- Confirm title, notes, and tag target are correct.
- Optional: if your repo uses release attestations, run `gh release verify vX.Y.Z`.

## Tagging Rules
- Use annotated tags: `git tag -a vX.Y.Z -m "vX.Y.Z" <commit>`.
- Tags should point at the release commit that includes the changelog update.
- Push tags explicitly: `git push origin vX.Y.Z`.

## Release Notes Style
- Lead with 2–4 key changes.
- Balance Added/Changed/Fixed where possible.
- Keep tone concise and user-focused.
