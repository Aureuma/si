---
title: Command Reference
description: Complete categorized reference for SI command families, major subcommands, and operator workflows.
---

# Command Reference

Use this page as the canonical command map for `si`.

## Command discovery workflow

```bash
si --help
si <command> --help
si <command> <subcommand> --help
```

## Core runtime commands

| Command family | Primary purpose | Major subcommands | Detailed guide |
| --- | --- | --- | --- |
| `si dyad` | Manage actor/critic pairs | `spawn`, `list`, `status`, `peek`, `exec`, `logs`, `start`, `stop`, `restart`, `remove`, `cleanup` | [Dyad](./DYAD) |
| `si codex` | Manage profile-bound Codex containers and profile registry state | `profile`, `spawn`, `start`, `stop`, `logs`, `status`, `tmux`, `respawn` | [CLI Reference](./CLI_REFERENCE) |
| `si vault` (`si creds`) | Encrypt and inject dotenv secrets | `keypair`, `status`, `check`, `hooks`, `encrypt`, `decrypt`, `restore`, `set`, `unset`, `get`, `list`, `run`, `docker exec` | [Vault](./VAULT) |
| `si fort` | Wrapper for hosted Fort policy/auth API (runtime secret access path) | `doctor`, `auth`, `get`, `set`, `list`, `batch-get`, `run`, `agent`, `config show`, `config set` | [Vault](./VAULT) |
| `si surf` | Dockerized Playwright MCP runtime | `build`, `start`, `status`, `logs`, `proxy` | [Browser](./BROWSER) |
| `si orbits` | Orbit registry and lifecycle | `list`, `install`, `update`, `enable`, `doctor`, `scaffold`, `policy`, `gateway (build)` | [Orbitals](./ORBITALS) |

## Provider and integration command families

| Integration | Command family | Typical first checks | Detailed guide |
| --- | --- | --- | --- |
| GitHub | `si github ...` | `si github auth status`, `si github doctor`, `si github project list Aureuma` | [GitHub](./GITHUB) |
| Cloudflare | `si cloudflare ...` | `si cloudflare auth status`, `si cloudflare doctor` | [Cloudflare](./CLOUDFLARE) |
| GCP + Gemini/Vertex | `si gcp ...` | `si gcp auth status`, `si gcp doctor` | [GCP](./GCP) |
| Google Places | `si google places ...` | `si google places auth status`, `si google places doctor` | [Google Places](./GOOGLE_PLACES) |
| Google Play | `si google play ...` | `si google play auth status`, `si google play doctor` | [Google Play](./GOOGLE_PLAY) |
| YouTube Data | `si google youtube ...` | `si google youtube auth status`, `si google youtube doctor` | [Google YouTube](./GOOGLE_YOUTUBE) |
| AWS | `si aws ...` | `si aws auth status`, `si aws doctor` | [AWS](./AWS) |
| OpenAI | `si openai ...` | `si openai auth status`, `si openai doctor` | [OpenAI](./OPENAI) |
| OCI | `si oci ...` | `si oci auth status`, `si oci doctor` | [OCI](./OCI) |
| Stripe | `si stripe ...` | `si stripe auth status`, `si stripe doctor` | [Stripe](./STRIPE) |
| Social APIs | `si social ...` | `si social <platform> auth status`, `doctor` | [Social](./SOCIAL) |
| WorkOS | `si workos ...` | `si workos auth status`, `si workos doctor` | [WorkOS](./WORKOS) |
| Apple App Store Connect | `si apple store ...` | `si apple store auth status`, `doctor` | [Apple App Store](./APPLE_APPSTORE) |
| Publish flows | `si publish ...` | `si publish catalog list` | [Publish](./PUBLISH) |
| Provider telemetry | `si providers ...` | `si providers characteristics`, `si providers health` | [Providers](./PROVIDERS) |

## Build, docs, and developer tooling

| Command family | Purpose | Typical usage |
| --- | --- | --- |
| `si build image` | Build local runtime image | `si build image` |
| `si-rs build self` | Build or upgrade `si` binary | `si-rs build self` |
| `si-rs build self assets` | Build all release archives + `checksums.txt` locally | `si-rs build self assets --version vX.Y.Z` |
| `si mintlify` | Docs lifecycle commands | `si mintlify validate`, `si mintlify dev` |
| `si analyze` (`si lint`) | Static analysis and validation lanes | `si analyze` |
| `si docker` | Raw Docker passthrough | `si docker ps` |
| `si persona` | Persona/profile helpers | `si persona <name>` |
| `si skill` | Skill role helper | `si skill <role>` |

## Recommended operator workflows

### 1. New machine bootstrap

```bash
si-rs build self
si vault status
si --help
si mintlify validate
```

### 2. Integration readiness check

```bash
si providers characteristics --json
si providers health --json
si github doctor --json
si cloudflare doctor --json
```

### 3. Release maintainer preflight

```bash
si-rs build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight
```

## Guardrails

- For host/admin automation, prefer `si vault run -- <cmd>` when a command needs secrets.
- For SI runtime containers, use `si fort ...` for secret access.
- `si fort` bootstrap/admin auth is file-backed and prefers explicit `--token-file` injection from `~/.si/fort/bootstrap/admin.token` when present.
- Pass native `fort` flags after `--` when invoking through wrapper.
- Run integration-specific `doctor` commands before write operations.
- Run `si mintlify validate` for docs changes and `si analyze` for code changes.
