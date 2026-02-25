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
| codex lifecycle (`si spawn`, `si run`, etc.) | Manage codex containers and one-off runs | `spawn`, `respawn`, `status`, `report`, `run`, `warmup` | [CLI Reference](./CLI_REFERENCE) |
| `si vault` (`si creds`) | Encrypt and inject dotenv secrets | `keypair`, `status`, `check`, `hooks`, `encrypt`, `decrypt`, `restore`, `set`, `unset`, `get`, `list`, `run`, `docker exec` | [Vault](./VAULT) |
| `si sun` | Cloud sync and machine orchestration for profiles, taskboards, and remote SI control | `auth`, `profile`, `token`, `audit`, `taskboard`, `machine`, `doctor` | [Sun Cloud Sync](./SUN) |
| `si browser` | Dockerized Playwright MCP runtime | `build`, `start`, `status`, `logs`, `proxy` | [Browser](./BROWSER) |
| `si plugins` | Plugin registry and lifecycle | `list`, `install`, `update`, `enable`, `doctor`, `scaffold`, `policy`, `gateway (build/push/pull/status)` | [Plugin Marketplace](./PLUGIN_MARKETPLACE) |

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
| Apple App Store Connect | `si apple appstore ...` | `si apple appstore auth status`, `doctor` | [Apple App Store](./APPLE_APPSTORE) |
| Publish flows | `si publish ...` | `si publish catalog list` | [Publish](./PUBLISH) |
| Provider telemetry | `si providers ...` | `si providers characteristics`, `si providers health` | [Providers](./PROVIDERS) |

## PaaS and operations commands

| Command family | Purpose | High-signal commands | Guide |
| --- | --- | --- | --- |
| `si paas` | Deploy and operate apps on SI PaaS | `doctor`, `deploy`, `logs`, `backup`, `events` | [PaaS Overview](./PAAS_OVERVIEW) |
| `si paas app` | App lifecycle controls | `list`, `status`, `env`, `deploy` | [PaaS Context Operations](./PAAS_CONTEXT_OPERATIONS_RUNBOOK) |
| `si paas backup` | Backup and restore workflows | `run`, `list`, `restore` | [PaaS Backup Policy](./PAAS_BACKUP_RESTORE_POLICY) |
| `si paas agent` | Automation agent runtime | `list`, `enable`, `run-once`, `logs` | [PaaS Agent Runtime Commands](./PAAS_AGENT_RUNTIME_COMMANDS) |

## Build, docs, and developer tooling

| Command family | Purpose | Typical usage |
| --- | --- | --- |
| `si build image` | Build local runtime image | `si build image` |
| `si build self` | Build or upgrade `si` binary | `si build self` |
| `si build self release-assets` | Build all release archives + `checksums.txt` locally | `si build self release-assets --version vX.Y.Z` |
| `si mintlify` | Docs lifecycle commands | `si mintlify validate`, `si mintlify dev` |
| `si analyze` (`si lint`) | Go static analysis | `si analyze --module tools/si` |
| `si docker` | Raw Docker passthrough | `si docker ps` |
| `si persona` | Persona/profile helpers | `si persona <name>` |
| `si skill` | Skill role helper | `si skill <role>` |

## Recommended operator workflows

### 1. New machine bootstrap

```bash
si build self
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

### 3. Pre-production PaaS check

```bash
si paas doctor --json
si paas backup run --app <slug> --json
si paas events tail --json
```

### 4. Release maintainer preflight

```bash
si build self release-assets --version vX.Y.Z --out-dir .artifacts/release-preflight
```

## Guardrails

- Prefer `si vault run -- <cmd>` for any command that needs secrets.
- Run integration-specific `doctor` commands before write operations.
- Run `si mintlify validate` for docs changes and `si analyze` for Go changes.
