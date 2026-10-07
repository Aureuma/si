---
title: CLI Reference
description: Practical SI CLI orientation with command discovery, top-level families, and high-signal workflows.
---

# CLI Reference

This page is the fast orientation guide for `si`.

For a full categorized list, use [Command Reference](./COMMAND_REFERENCE).

## Command discovery pattern

```bash
si --help
si <command> --help
si <command> <subcommand> --help
```

## CLI color system

SI runtime text output uses a small semantic palette instead of per-command ad hoc colors:

| Role | Meaning | Color |
| --- | --- | --- |
| Commands and examples | command names, runnable examples, selected profiles | magenta |
| Labels | `key=value` keys, field names, probe labels | blue |

Clap-rendered `--help` output keeps its own styled headers, usage lines, and literals.

Rules:

- JSON output stays uncolored.
- Text output uses the semantic palette above when color is enabled.
- `si --help` and nested `--help` output use the same palette as runtime text output.

Color control:

- `SI_CLI_COLOR=always`: force color even when stdout is not a TTY
- `SI_CLI_COLOR=auto`: default behavior
- `SI_CLI_COLOR=never`: disable CLI colors
- `NO_COLOR=1`: disable CLI colors

## Top-level command families

| Domain | Commands |
| --- | --- |
| Runtime and orchestration | `si codex`, `si muse` |
| Settings | `si settings` (dotenv encryption lives in the separate `vault` binary) |
| Third-party integrations | Use the standalone `orbit <provider> ...` CLI from `Aureuma/orbit`. |
| Meta and diagnostics | `si commands`, `si version`, `si help`, `si doctor` |

## High-signal workflows

### Runtime setup

```bash
si codex spawn --profile default --workspace "$PWD"
si codex stop --profile default --slot review
si codex list
```

### Onboarding two profiles per provider

Enroll each real account once under its own profile id, then log in.
Browser/device approval during `login` is the only expected manual step.

```bash
# Codex accounts A and B
si codex profile add work --email work@example.com
si codex profile add personal --email personal@example.com
si codex profile login work
si codex profile login personal

# Muse accounts A and B (Linux only; see Troubleshooting)
si muse profile add work --email work@example.com
si muse profile add personal --email personal@example.com
si muse profile login work
si muse profile login personal
```

Verify isolation before doing real work:

```bash
si codex profile list
si muse profile list
si codex spawn --profile work --workspace "$PWD"
si codex spawn --profile personal --workspace "$PWD"
si codex list
```

Rules the CLI enforces:

- Each profile owns a canonical native store; A's credential lifecycle never
  touches B's identity or store.
- `login` binds the observed identity to the profile. A mismatch disables
  launch until you re-login; SI never deletes native state to "fix" it.
- Do not clone one account's auth into two profiles and call that a
  two-account setup.
- Profile ids and slots are validated slugs; slots default to `primary`.

### Integration readiness

```bash
orbit list --json
orbit github doctor --json
orbit cloudflare doctor --json
orbit gcp doctor --json
```

### Release preflight

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- self assets --out-dir .artifacts/release-preflight
orbit github release create Aureuma/si --tag vX.Y.0 --title "vX.Y.0" --target "$(git rev-parse HEAD)" --draft
```

- `si-release self assets` defaults to the canonical SI workspace version from root `Cargo.toml`.
- For SI itself, release tags come from that same repo-wide version and only minor releases are tagged/published.
- `orbit github release create` now verifies the remote tag first.
- When the tag is missing, pass `--target <sha>` and SI will create the git tag ref before creating the release.
- For draft releases, GitHub may still return an `untagged-...` HTML URL until publish; verify with `tag_name` and `git ls-remote --tags`.

### Faster Rust iteration

```bash
cargo check -p si-cli --locked
./tools/si-dev --help
```

- `./tools/si-dev` rebuilds the release `si` binary only when Rust sources change, then execs it.
- `cargo check` typechecks the SI CLI workspace without linking a release binary.
- Keep SI's `.artifacts/cargo-target` warm during active development. Prune it only when artifacts are older than 14 days or when root disk pressure requires immediate recovery.

## Safety guidance

- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep command docs aligned with `si --help` and `si help --format json`.
`orbit github release create` now follows the checkout-first default path more closely:
- omit the repo argument inside a GitHub checkout and SI infers it from `origin`
- use `-R, --repo <owner/repo>` when you need an explicit override
- omit `--title` to reuse the release tag as the title
