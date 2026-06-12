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
| `si codex` | Manage profile-bound Codex workers and profile registry state | `profile`, `spawn`, `stop`, `remove`, `tail`, `shell`, `list`, `tmux`, `warmup`, `respawn` | [CLI Reference](./CLI_REFERENCE) |
| `si vault` (`si creds`) | Encrypt and inject dotenv secrets | `keypair`, `status`, `check`, `hooks`, `encrypt`, `decrypt`, `restore`, `set`, `unset`, `get`, `list`, `run` | [Vault](./VAULT) |
| `si fort` | Wrapper for hosted Fort policy/auth API | `doctor`, `auth`, `get`, `set`, `list`, `batch-get`, `run`, `agent`, `config show`, `config set` | [Vault](./VAULT) |
| `si surf` | Local Playwright browser runtime | `build`, `start`, `status`, `logs`, `proxy` | [Browser](./BROWSER) |
| `si viva` | Manage the Viva runtime wrapper | `config`, passthrough runtime helpers | [CLI Reference](./CLI_REFERENCE) |
| `si image` | Image provider and generation bridge | provider-specific image flows | [CLI Reference](./CLI_REFERENCE) |
| `si settings` | Show resolved SI settings | none | [Settings](./SETTINGS) |
| `si doctor` | Check fresh-machine distribution prerequisites | `--format json` | [Testing](./testing) |
| `si commands` | List visible SI root commands | `list` | [CLI Reference](./CLI_REFERENCE) |

## Third-party integrations

Third-party API/provider command families moved to the standalone `Aureuma/orbit` repository and `orbit <provider> ...` CLI. SI no longer owns those command implementations or provider settings.

## Integration ownership

| Integration | Ownership mode | SI responsibility | Owning implementation |
| --- | --- | --- | --- |
| `fort` | `native-wrapper` | Resolve/build/run the Fort binary, mediate SI auth/session paths, and expose wrapper config | `fort` repo |
| `viva` | `native-wrapper` | Resolve/build/run the Viva binary and manage SI wrapper/tunnel settings | `viva` repo |
| `surf` | `native-wrapper` | Resolve/build/run the Surf binary, set `SI_SURF_WRAPPED=1`, and manage SI wrapper settings | `surf` repo |
| `image` | `internal-si` / provider bridge | Keep SI-owned image bridge commands in SI | SI repo |
| `vault` | `internal-si` maintenance | Maintain SI Vault encryption/checking flows; prefer Fort for operator secret runtime work | SI repo |
| third-party providers | `provider-client` | No SI root command; use `orbit <provider> ...` | `orbit` repo |
| first-party API clients | `api-client` when present | Thin CLI/request formatting only when SI explicitly owns the client | Owning service backend |
| catalog-only providers | inventory-only | Do not document as runnable SI command families | Owning catalog/orbit data |

If a standalone adjacent repo owns a stable CLI, add a root SI wrapper only for wrapper concerns. If a service exposes an HTTP API but no stable local CLI, keep business logic, schemas, billing/usage rules, and backend tests in the owning service.

## Build, docs, and developer tooling

| Command family | Purpose | Typical usage |
| --- | --- | --- |
| `si build self` | Build or upgrade `si` binary | `si build self` |
| `si build self check` | Fast typecheck for the SI CLI | `si build self check --timings` |
| `si build self assets` | Build all release archives + `checksums.txt` locally | `si build self assets --out-dir .artifacts/release-preflight` |
| `si commands` | Show visible public root commands | `si commands` |
| `si settings` | Inspect resolved settings | `si settings` |

## Recommended operator workflows

### 1. New machine bootstrap

```bash
si codex status
si build self
si build self check --timings
si vault status
si --help
si commands
```

### 2. Release maintainer preflight

```bash
si build self assets --out-dir .artifacts/release-preflight
```

## Guardrails

- Use `si fort` and `si codex` commands directly for task, worker, session, and run control.
- For host/admin automation, prefer `si fort run -- <cmd>` when a command needs secrets.
- For SI runtime workers, use `si fort ...` for secret access.
- Pass native `fort` flags after `--` when invoking through wrapper.
- Run `si help --format json` or `si commands` when updating CLI docs.
