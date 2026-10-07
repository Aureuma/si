---
title: Troubleshooting
description: Auth-state labels, credential renewal, lease and tmux behavior, migration, and the Linux/Darwin support matrix.
---

# Troubleshooting

## Auth-state labels

SI never claims a profile is logged in from files alone. Profile status uses
these exact labels:

| Label | Meaning | Next step |
| --- | --- | --- |
| `not_logged_in` | No credential material in the profile store | `si <provider> profile login <id>` |
| `credential_present_unverified` | Credential files exist but no native result confirms them | Run a native command or `login` to verify |
| `authenticated` | A native result confirmed the account | None |
| `reauth_required` | The provider rejected the stored credential | `si <provider> profile login <id>` again |
| `network_failure` | The status probe failed on transport | Check network, retry |
| `quota_exhausted` | The provider reports no remaining quota | Wait for reset, check plan |
| `unsupported_backend` | The credential store backend is not the SI-enforced one | Re-enroll the profile |
| `unknown` | The probe returned an unrecognized outcome | Re-run with `--format json`, report the detail |

Identity binding adds one more marker: `identity_unverified` means SI recorded
an account hint (for example an email from a local token claim) but has not
yet confirmed it against a native result. It clears on the next verified
login; a mismatch disables launch for that profile until you re-login.

## Credential renewal and re-login

- Renewal is native: when a provider rotates tokens inside a worker session,
  the new credential stays in that profile's canonical store. SI never copies
  it elsewhere and never overwrites it with another profile's credential.
- If a launch reports `reauth_required`, re-run
  `si <provider> profile login <id>` and approve in the browser/device flow.
  That is the only expected manual step.
- `si <provider> profile logout <id>` clears the native credential for that
  profile only. Other profiles are untouched.
- History persists: stopping or removing a worker never deletes native
  session history; `remove` refuses live workers unless passed `--stop`.

## Lease and concurrency errors

SI holds a per-profile worker lease during credential use. A second launch on
the same profile while one is active fails with a lease-busy error instead of
forking credentials into more homes. Same-profile concurrency stays disabled
until native locking behavior is proven per provider.

If you see a busy-lease error:

1. `si <provider> list` to find the active worker.
2. `si <provider> stop --profile <id> --slot <slot>` (or `--force`).
3. Retry the launch.

## tmux behavior

- Every worker runs on an SI-owned socket at `<si-root>/runtime/tmux.sock`,
  never on your default tmux server.
- `si <provider> attach` execs into the session and requires a TTY.
- `tail` works on live sessions and on dead panes kept with remain-on-exit.
- Foreign sessions with colliding names are reported as conflicts, never
  replaced.

## Migration from pre-2.0 stores

Profiles created before schema 2 carry legacy records. Commands refuse them
with an exact remediation:

```bash
si doctor --migration-plan   # dry run, changes nothing
si doctor --migrate          # validate-first apply, idempotent
```

Migration keeps native auth, history, and config files byte-identical in
place and records a private metadata-only backup. A second run changes
nothing.

## Removed commands

`si fort`, `si surf`, `si viva`, `si image`, `si build`, and
`si codex warmup` were removed in the multi-account refocus. The root names
fail fast with a pointer to `si --help`:

```text
Error: `si fort` was removed; use `si --help` instead
```

Release and packaging flows moved to the dev-only `si-release` tool
(`cargo run -p si-tools --bin si-release -- ...`), which is never shipped.

## Platform support matrix

| Provider | Linux | macOS | Other |
| --- | --- | --- | --- |
| Codex multi-account | Supported | Native-dependent (no SI gate) | Native-dependent |
| Muse multi-account | Supported | Refused: native Keychain-backed auth cannot be isolated per profile yet | Refused |

`si doctor` reports a per-provider verdict plus the platform gate outcome.
Codex reports `missing` (not on `PATH`) or `managed` (file-store forcing
applies to any native version). Muse reports `missing`, `supported` (the
exact tested native), or `unverified` for anything else — never `supported`;
re-run the auth-destination checks before trusting isolation on a new native
version.

## Getting help output

```bash
si --help
si <command> --help
si help --format json
si commands
```
