---
title: Command Reference
description: Complete categorized reference for SI command families, major subcommands, and operator workflows.
---

# Command Reference

Use this page as the canonical command map for `si`.

## Command discovery workflow

```bash
si --help
si help
si version
si <command> --help
si <command> <subcommand> --help
```

Color semantics for help and text-mode output are documented in [CLI Reference](./CLI_REFERENCE#cli-color-system). JSON output remains uncolored by design.

## Core runtime commands

| Command family | Primary purpose | Major subcommands | Detailed guide |
| --- | --- | --- | --- |
| `si codex` | Manage profile-bound Codex workers and profile registry state | `profile`, `spawn`, `stop`, `remove`, `tail`, `shell`, `run`, `list`, `tmux`, `respawn` | [CLI Reference](./CLI_REFERENCE) |
| `si muse` | Manage profile-bound Muse workers and profile registry state | `profile`, `spawn`, `stop`, `remove`, `tail`, `shell`, `run`, `list`, `tmux`, `respawn` | [CLI Reference](./CLI_REFERENCE) |
| `si settings` | Show resolved SI settings | none | [Settings](./SETTINGS) |
| `si doctor` | Check fresh-machine distribution prerequisites | `--format json` | [Testing](./testing) |
| `si commands` | List visible SI root commands | `list` | [CLI Reference](./CLI_REFERENCE) |

## Third-party integrations

Third-party API/provider command families moved to the standalone `Aureuma/orbit` repository and `orbit <provider> ...` CLI. SI no longer owns those command implementations or provider settings.

## Integration ownership

| Integration | Ownership mode | SI responsibility | Owning implementation |
| --- | --- | --- | --- |
| third-party providers | `provider-client` | No SI root command; use `orbit <provider> ...` | `orbit` repo |
| first-party API clients | `api-client` when present | Thin CLI/request formatting only when SI explicitly owns the client | Owning service backend |
| catalog-only providers | inventory-only | Do not document as runnable SI command families | Owning catalog/orbit data |

If a standalone adjacent repo owns a stable CLI, add a root SI wrapper only for wrapper concerns. If a service exposes an HTTP API but no stable local CLI, keep business logic, schemas, billing/usage rules, and backend tests in the owning service.

## Introspection and developer tooling

| Command family | Purpose | Typical usage |
| --- | --- | --- |
| `si commands` | Show visible public root commands | `si commands` |
| `si settings` | Inspect resolved settings | `si settings` |
| `si-release` (dev-only) | Release-asset and installer tooling, never shipped | `cargo run -p si-tools --bin si-release -- self assets --out-dir .artifacts/release-preflight` |

## Recommended operator workflows

### 1. New machine bootstrap

```bash
si doctor
cargo build -p si-cli --release --locked
si --help
si commands
```

### 2. Release maintainer preflight

```bash
cargo run --quiet --locked -p si-tools --bin si-release -- self assets --out-dir .artifacts/release-preflight
```

## Guardrails

- Use `si codex` and `si muse` commands directly for task, worker, session, and run control.
- Run `si help --format json` or `si commands` when updating CLI docs.
