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

## Tagging Rules
- Use annotated tags: `git tag -a vX.Y.Z -m "vX.Y.Z" <commit>`.
- Tags should point at the release commit that includes the changelog update.
- Push tags explicitly: `git push origin --tags`.

## Release Process
1. Update `CHANGELOG.md` with the new version entry.
2. Run tests and record any known issues.
3. Commit the changelog/release notes.
4. Create an annotated tag for the release commit.
5. Push commits and tags.
6. Publish the GitHub release using the tag and changelog notes.

## Release Notes Style
- Lead with 2-4 key changes.
- Balance Added/Changed/Fixed where possible.
- Keep tone concise and user-focused.
