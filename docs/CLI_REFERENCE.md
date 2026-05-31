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

SI text output uses a small semantic palette instead of per-command ad hoc colors:

| Role | Meaning | Color |
| --- | --- | --- |
| Section headings | usage blocks, help sections, command-group titles | cyan |
| Commands and examples | command names, runnable examples, selected profiles | magenta |
| Flags and operator prompts | options, warnings, confirmation prompts | yellow |
| Labels | `key=value` keys, field names, probe labels | blue |
| Success | ready, ok, warmed, healthy state | green |
| Warning | degraded or operator-attention state | yellow |
| Error | failed, invalid, destructive/error state | red |
| Muted | indexes, separators, filler text | gray |

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
| Runtime and orchestration | `si nucleus`, `si codex`, `si surf`, `si viva` |
| Secrets and context | `si vault` (`si creds`), `si fort`, `si settings` |
| Third-party integrations | Use the standalone `orbit <provider> ...` CLI from `Aureuma/orbit`; `si image` remains in SI for image bridge workflows. |
| Build and release | `si build`, `si commands`, `si version`, `si help` |

## High-signal workflows

### Nucleus control plane

```bash
si nucleus status
si nucleus profile list
si nucleus task create "Inspect blocked task" "Summarize the current blocked reason and latest checkpoint."
si nucleus task cancel <task-id>
si nucleus task list
si nucleus task prune --older-than-days 30
si nucleus worker repair-auth <worker-id>
si nucleus service install
si nucleus service status --format json
si nucleus events subscribe --count 1
```

### Runtime setup

```bash
si codex spawn --profile default --workspace "$PWD"
si codex stop --profile default --slot review
si codex list
```

### Viva tunnel via SI wrapper

```bash
si viva config set --repo ~/Development/viva --build true
si viva config tunnel show --json
si viva -- tunnel up --profile dev
si viva -- tunnel status --profile dev
si viva -- tunnel down --profile dev
```

### Integration readiness

```bash
orbit list --json
orbit github doctor --json
orbit cloudflare doctor --json
orbit gcp doctor --json
```

### Fort runtime secret check

```bash
si fort doctor
si fort get --repo github --env dev --key RM_GITHUB_TOKEN
```

### Release preflight

```bash
si build self assets --out-dir .artifacts/release-preflight
orbit github release create Aureuma/si --tag vX.Y.0 --title "vX.Y.0" --target "$(git rev-parse HEAD)" --draft
```

- `si build self assets` defaults to the canonical SI workspace version from root `Cargo.toml`.
- For SI itself, release tags come from that same repo-wide version and only minor releases are tagged/published.
- `orbit github release create` now verifies the remote tag first.
- When the tag is missing, pass `--target <sha>` and SI will create the git tag ref before creating the release.
- For draft releases, GitHub may still return an `untagged-...` HTML URL until publish; verify with `tag_name` and `git ls-remote --tags`.

### Faster Rust iteration

```bash
si build self check --timings
si build self --timings
```

- `si build self` now reuses `.artifacts/cargo-target/self-build` by default for faster rebuilds.
- `si build self check` runs `cargo check` against the SI CLI manifest without linking a release binary.
- `si build self` and release-asset builds auto-use `sccache` when it is available on `PATH`.
- Keep SI's `.artifacts/cargo-target` warm during active development. Prune it only when artifacts are older than 14 days or when root disk pressure requires immediate recovery; clear repo target directories before clearing `sccache` so cross-repo Rust rebuilds stay fast.

## Safety guidance

- For Nucleus deployments protected by `SI_NUCLEUS_AUTH_TOKEN`, all gateway and REST operations require that bearer token; use the same token from CLI clients.
- CLI endpoint discovery for `si nucleus ...` resolves from `--endpoint`, then `SI_NUCLEUS_WS_ADDR`, then `~/.si/nucleus/gateway/metadata.json`, then the default local websocket URL.
- On host/admin flows, use `si vault run -- <command>` when secrets are required.
- For SI runtime workers, use `si fort ...` for secret access.
- `si fort` wrapper passes explicit Fort file-path auth flags for the managed Codex profile session under `CODEX_HOME/fort/`; caller-supplied `FORT_TOKEN_PATH` / `FORT_REFRESH_TOKEN_PATH` values are not normal runtime fallbacks.
- Runtime secret commands fail loudly when no usable runtime Fort session exists; bootstrap/admin token files are only for explicit admin/provisioning commands.
- If a flag belongs to the native `fort` CLI, pass it after `--` (example: `si fort -- --host https://fort.aureuma.ai doctor`).
- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep command docs aligned with `si --help` and `si help --format json`.
`orbit github release create` now follows the checkout-first default path more closely:
- omit the repo argument inside a GitHub checkout and SI infers it from `origin`
- use `-R, --repo <owner/repo>` when you need an explicit override
- omit `--title` to reuse the release tag as the title
