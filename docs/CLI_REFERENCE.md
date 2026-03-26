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
| Runtime and orchestration | `si dyad`, `si codex` |
| Secrets and context | `si vault` (`si creds`), `si fort` |
| Integration bridges | `si github`, `si cloudflare`, `si gcp`, `si aws`, `si openai`, `si oci`, `si google`, `si social`, `si workos`, `si apple store`, `si stripe`, `si publish`, `si releasemind` (`si release`) |
| Provider telemetry | `si providers` |
| Surf browser runtime | `si surf` |
| Orbit ecosystem | `si orbits` |
| Build and quality | `si build`, `si analyze` (`si lint`), `si docker` |
| Docs workflow | `si mintlify` |
| Profiles and skills | `si persona`, `si skill` |

## High-signal workflows

### Runtime setup

```bash
si build image
si dyad spawn start --name app-hardening --workspace "$PWD"
si dyad status app-hardening
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
si providers characteristics --json
si github doctor --json
si cloudflare doctor --json
si gcp doctor --json
```

### Fort runtime secret check

```bash
si fort doctor
si fort get --repo releasemind --env dev --key RM_OPENAI_API_KEY
```

### Docs quality

```bash
si mintlify validate
si mintlify broken-links
```

### Release preflight

```bash
./.artifacts/cargo-target/release/si-rs build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight
```

## Safety guidance

- On host/admin flows, use `si vault run -- <command>` when secrets are required.
- In SI runtime containers, use `si fort ...` for secret access.
- `si fort` wrapper passes explicit Fort file-path auth flags when defaults are available: `--host` from settings and `--token-file` from `~/.si/fort/bootstrap/admin.token`.
- If a flag belongs to the native `fort` CLI, pass it after `--` (example: `si fort -- --host https://fort.aureuma.ai doctor`).
- Prefer `--json` for automation and auditability.
- Run `doctor` commands before mutating production systems.
- Keep docs and `docs.json` navigation in sync.
