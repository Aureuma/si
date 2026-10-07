# SI live acceptance report (redacted)

Date: 2026-10-07. SI: v0.60.33 (`sic/codex-muse-refocus-release`).
Natives: `codex-cli 0.160.0`, `Muse Code 1.4.3 (1.4.3-R5018.1)` (tested pin).
Host: Linux. Live state contained under `SI_HOME=$HOME/.si-live-test`
(real accounts, real isolation, zero writes to `~/.si`, `~/.codex`,
or the default Muse XDG paths).

This report contains assertions and redacted identity labels only. No
tokens, emails, device codes, tmux captures, or auth-file contents.

## Account inventory (provider evidence, redacted)

- Codex host account: PRESENT and live. `codex login status` in the
  default home reports a ChatGPT login. Second Codex account: ABSENT.
- Muse host account: PRESENT. Default XDG `auth.json` holds a
  `providers.meta` entry with account fields. Second Muse account: ABSENT.

## Enrollment attempts (D13)

- `si codex profile add live-a` / `si muse profile add live-a`: OK
  (offline registry writes; `not_logged_in`; canonical native roots
  under the live-test SI home).
- `si codex profile login live-a`: reached the native device-code flow
  (browser URL + one-time code, 15-minute expiry), then killed before
  approval. Store verified clean afterwards: no `auth.json`, still
  `not_logged_in`. BLOCKED on manual account-holder approval.
- `si muse profile login live-a`: reached the native Meta device-OAuth
  flow (auth URL + short code, "Waiting for approval"), then abandoned.
  Store verified clean afterwards (one diagnostic tracing log only),
  still `not_logged_in`. BLOCKED on manual account-holder approval.

No cloned-account or fake-token enrollment was attempted (forbidden).

## Live matrix (D14/D15) via `si-live-accounts`

New dev-only runner `si-tools --bin si-live-accounts` implements the
section 12 legs: enrollment gate (rejects identical stores/identities),
simultaneous overlap barrier, tiny marker round-trips, respawn-a,
logout-a, relogin-a, renewal, revocation, session-association,
concurrency refusal, subprocess binding. Output is redacted; exit 1 on
any failed leg, exit 0 when everything is blocked-but-honest.

Verified runner behavior on this host (both providers):

- Unenrolled A/B: `enrollment` blocked with the exact auth states; all
  10 remaining legs blocked with "requires two confirmed distinct live
  accounts". Exit 0. No fake passes.
- Same profile for A and B: `enrollment` FAILS with identical-store
  refusal. Exit 1.
- Redaction unit tests: token/JWT shapes stripped (including
  `key=value` forms), labels and profile paths preserved.

All real-request legs: BLOCKED — no two authenticated distinct accounts
exist to run them against.

## Gate verdicts

| Gate | Status | Unblock requirement |
| --- | --- | --- |
| Two distinct live Codex accounts | INCOMPLETE | Second authorized ChatGPT account + holder approval of `si codex profile login live-b` (and `live-a`) |
| Two distinct live Muse accounts | INCOMPLETE | Second authorized Meta account + holder approval of `si muse profile login live-b` (and `live-a`) |
| Simultaneous real requests | BLOCKED | Above, then `si-live-accounts --provider <p> --profile-a live-a --profile-b live-b` |
| Marker round-trips | BLOCKED | Same |
| Stop/respawn, logout/re-login, renewal, revocation | BLOCKED | Same; renewal additionally needs a genuine refresh event or provider test hook |
| Session association | BLOCKED | Same |
| Same-profile concurrency | DISABLED BY DESIGN | Proven refusal is asserted by the runner once accounts exist; enabling needs native-locking proof |
| Failed login never falls back | PASS (observed) | Killed/abandoned logins left both profile stores `not_logged_in` with no credential writes |

## Compatibility notes

- Muse multi-account remains Linux-only; macOS refuses at the
  documented Keychain isolation gate (no Darwin host available here to
  re-verify; gate is unit-tested).
- Unverified native versions report `unverified`, never `supported`.
- Same-profile concurrency stays disabled; the runner asserts refusal.
