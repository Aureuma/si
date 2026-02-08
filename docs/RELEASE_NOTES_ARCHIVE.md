# Release Notes Archive

This archive preserves release notes after renumbering onto the `v0.x` line.

## v0.39.0

Title: `Silex Primus v0.39.0`

Published: `2026-01-26T18:40:01Z`

```
Silex Primus marks a major milestone for Silexa.

Highlights:
- ✨ Codex profiles are now first-class: `si codex profile` lists identities and `si codex login` can target a profile.
- 🔐 Profile logins cache auth on the host, and `si codex spawn --profile` seeds auth into containers automatically.
- 📁 Workspace mounts now default to the current directory when `--workspace` isn’t provided (codex spawn/respawn and dyad spawn).
- 🧰 CLI output polish: Unicode-aware column widths and safer config/auth seeding paths.
- 📜 New `CHANGELOG.md` and `docs/RELEASING.md` formalize release history and process.

In the margins: 1·26·2026 · v0.39.0

Full details are in CHANGELOG.md.
```

## v0.40.0

Title: `v0.40.0 — Unified image runtime`

Published: `2026-01-27T15:57:37Z`

```
## Highlights
- Shipped a single `silexa/silexa:local` image for codex containers and dyad runtimes.
- Updated defaults and runtime entrypoints for the unified image.
- Removed the separate base/codex/actor/critic image definitions.
- Refreshed CLI help and docs for the new image layout and flag ordering.
```

## v0.41.0

Title: `v0.41.0 - Consolidated Release`

Published: `2026-01-30T18:04:30Z`

```
Release v0.41.0 consolidates recent improvements to the login flow and release tooling.

Highlights:
- Added automatic login URL opening with Safari profile support and overrides.
- Added automatic device code copy to clipboard on macOS and Linux.
- Improved login URL parsing to strip ANSI/escape artifacts.
- Documented a clearer end-to-end release process and GitHub Release workflow.
- Added codex/dyad docker socket mount toggles and settings defaults.
```
