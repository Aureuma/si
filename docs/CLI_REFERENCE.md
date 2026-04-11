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
| Provider orbits | `si orbit github`, `si orbit cloudflare`, `si orbit gcp`, `si orbit aws`, `si orbit openai`, `si orbit oci`, `si orbit google`, `si orbit workos`, `si orbit apple`, `si orbit stripe`, `si image` |
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
si orbit list --json
si orbit github doctor --json
si orbit cloudflare doctor --json
si orbit gcp doctor --json
```

### Fort runtime secret check

```bash
si fort doctor
si fort get --repo releasemind --env dev --key RM_OPENAI_API_KEY
```

### Release preflight

```bash
si build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight
si orbit github release create Aureuma/si --tag vX.Y.Z --title "vX.Y.Z" --target "$(git rev-parse HEAD)" --draft
```

- `si orbit github release create` now verifies the remote tag first.
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

## Safety guidance

- For Nucleus gateway writes beyond loopback, set `SI_NUCLEUS_AUTH_TOKEN` and use the same bearer token from CLI clients.
- CLI endpoint discovery for `si nucleus ...` resolves from `--endpoint`, then `SI_NUCLEUS_WS_ADDR`, then `~/.si/nucleus/gateway/metadata.json`, then the default local websocket URL.
- On host/admin flows, use `si vault run -- <command>` when secrets are required.
- For SI runtime workers, use `si fort ...` for secret access.
- `si fort` wrapper passes explicit Fort file-path auth flags when defaults are available: runtime token paths from `FORT_TOKEN_PATH` / `FORT_REFRESH_TOKEN_PATH`, then `CODEX_HOME/fort/`, then the active Codex profile Fort session.
- Runtime secret commands fail loudly when no usable runtime Fort session exists; bootstrap/admin token files are only for explicit admin/provisioning commands.
- If a flag belongs to the native `fort` CLI, pass it after `--` (example: `si fort -- --host https://fort.aureuma.ai doctor`).
- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep command docs aligned with `si --help` and `si help --format json`.
