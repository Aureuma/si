# SI Repository Rules
## Release Discipline
- Use one single SI repository version across the whole system rather than separate versions for the gateway, REST API, storage schema, SDK surfaces, or other SI-owned runtime layers.
- Keep exactly one hard-coded source of truth for that SI version: root `Cargo.toml` under `[workspace.package].version`.
- Rust crates, packaging flows, release tooling, docs, and examples must derive from that root workspace version instead of maintaining independent SI version values.
- Every commit that changes SI must bump the workspace patch version in the same commit. Do not batch multiple commits under one unchanged patch version and do not defer the bump.
- Bump the workspace minor version only when publishing a new SI release to GitHub Releases or another distribution channel such as npm or Homebrew. Reset the patch component to `0` in that release commit.
- Create git tags only for those minor release commits; do not tag patch-only commits.
- SI release tags must use the canonical `vX.Y.0` form. Do not create bare tags such as `0.50.0`, and do not mix `v`-prefixed and bare tags for the same release line.
- GitHub Releases must exist only for those minor-release tags. Do not publish GitHub Releases for patch versions such as `v0.46.1`, and do not leave duplicate release entries pointing at both `0.50.0` and `v0.50.0`.
- When cleaning up release history, preserve the canonical `vX.Y.0` release/tag entry and remove non-canonical patch or bare-tag release entries first.
- Release notes for each minor release must cover every commit and patch-version bump since the previous minor release.
- When the SI version changes, that one version change applies everywhere in SI at once.
- After bumping the version, rebuild the SI binary on this host and update the mapped installed locations that SI uses, including the repo-local binary and the host-installed binary when applicable.
- Prefer rebuild paths that reuse cached Cargo artifacts so incremental follow-up builds stay fast.
# Secrets And Credential Access
- Raw `si vault` use in this repo is limited to Fort/SI Vault implementation or maintenance work. Operator secret access still goes through `si fort`.
