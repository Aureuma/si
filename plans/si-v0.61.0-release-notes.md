# SI v0.61.0 release notes (DRAFT — not published)

Target tag: `v0.61.0` (canonical form; no bare `0.61.0` tag, no patch releases).
Target commit: the `SiC: release v0.61.0` commit on `sic/codex-muse-refocus-release`
after review + merge to `main`.

## Highlights

SI is now a multi-account agent runtime for Codex and Muse: two genuinely
isolated accounts per provider, each in its own canonical native store, with
an owned tmux socket, per-profile credential leases, and native-verified
identity binding. Everything else (Fort/Surf/Viva/Image/Build/Warmup) is gone.

## Breaking changes

- Removed `si fort`, `si surf`, `si viva`, `si image`, `si build`;
  `si codex warmup`, `si codex status`, `si codex profile swap`,
  `si codex repair-auth`. Removed roots fail fast with a pointer to
  `si --help`. Release/packaging moved to dev-only `si-release`.
- Removed `[fort]`/`[viva]`/`[surf]` settings (leftovers ignored).
- Settings schema 2; pre-2 records need `si doctor --migrate`.
- Muse multi-account is Linux-only (macOS Keychain gate).
- Same-profile concurrency stays disabled; second worker refused.

## Migration

See CHANGELOG "Migration to v0.61.0" + `docs/TROUBLESHOOTING.md`.
Upgrade never deletes user data on disk.

## Known gaps at release time (honest)

- Two-account live acceptance (section 12 matrix) is INCOMPLETE: no second
  Codex/Meta accounts on the validation host and no manual approvals.
  The `si-live-accounts` runner exists and reports honestly; run it once
  accounts are available. Single-account login flows were exercised to
  the device-approval surface; stores left clean.
- Multi-target `self assets` needs CI cross toolchains (no local aarch64
  linker); host asset verified locally.
- Homebrew smoke skipped locally (no brew); CI macOS lane owns it.

## Commits since v0.60.0 (every patch bump)

- 8c3f4586 Phase A1 docs + plan tracking (0.60.20)
- 36d8b668 Phase A3 si-release extraction (0.60.21)
- c576f56e Dyad removal recorded in plan (0.60.22)
- 37914e41 Dyad profiles dir deleted (0.60.23)
- 35fefa0c Phase A4 isolated baseline (0.60.24)
- 1076b0c2 Phase B5 shared core (0.60.25)
- 01c64b9d Phase B6 Codex correction (0.60.26)
- 89a630bb Phase B7 tmux runner + migration (0.60.27)
- eb06838b tmux test-isolation rules doc (no bump; pre-existing)
- 85e3b3c0 Phase B8 credential protection (0.60.28)
- 3060533e Phase C9 Muse adapter (0.60.29)
- 7a55796d Phase C10 isolation via SI (0.60.30)
- c25f75bf Phase C11 product cuts (0.60.31)
- 82f75ca1 Phase C12 docs + changelog (0.60.32)
- ef7e5fa1 C11 deletion follow-up (0.60.33)
- 4a4eac43 Phase D13-16 live runner + report (0.60.34)
- 07a89ed5 Phase E17 validation + smoke fixes (0.60.35)
- (this commit) release v0.61.0 (0.61.0)

## Publish checklist (human)

1. Review + merge this branch to `main`.
2. Tag `v0.61.0` on the merge/release commit; push tag.
3. `si-release self assets` + verify; attach to GitHub Release `v0.61.0`.
4. Publish npm + Homebrew formulae (tokens required).
5. Run `si-live-accounts` for both providers once second accounts exist.
