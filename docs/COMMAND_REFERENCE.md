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

Color semantics for help and text-mode output are documented in [CLI Reference](./CLI_REFERENCE#cli-color-system). JSON output remains uncolored by design.

## Core runtime commands

| Command family | Primary purpose | Major subcommands | Detailed guide |
| --- | --- | --- | --- |
| `si nucleus` | Local control plane for tasks, workers, sessions, runs, gateway inspection, and service management | `status`, `profile`, `service`, `task`, `worker`, `session`, `run`, `events` | [Nucleus](./NUCLEUS) |
| `si codex` | Manage profile-bound Codex workers and profile registry state | `profile`, `spawn`, `remove`, `tail`, `shell`, `list`, `tmux`, `warmup`, `respawn` | [CLI Reference](./CLI_REFERENCE) |
| `si vault` (`si creds`) | Encrypt and inject dotenv secrets | `keypair`, `status`, `check`, `hooks`, `encrypt`, `decrypt`, `restore`, `set`, `unset`, `get`, `list`, `run` | [Vault](./VAULT) |
| `si fort` | Wrapper for hosted Fort policy/auth API (runtime secret access path) | `doctor`, `auth`, `get`, `set`, `list`, `batch-get`, `run`, `agent`, `config show`, `config set` | [Vault](./VAULT) |
| `si surf` | Local Playwright browser runtime | `build`, `start`, `status`, `logs`, `proxy` | [Browser](./BROWSER) |
| `si viva` | Manage Viva runtime and node helper commands | `config`, passthrough runtime helpers | [CLI Reference](./CLI_REFERENCE) |
| `si orbit` | First-party provider orbit namespace | `list`, `github`, `cloudflare`, `aws`, `gcp`, `google`, `openai`, `oci`, `stripe`, `workos`, `apple` | [Providers](./PROVIDERS) |
| `si image` | Image provider and generation bridge | provider-specific image flows | [Providers](./PROVIDERS) |
| `si settings` | Show resolved SI settings | none | [CLI Reference](./CLI_REFERENCE) |
| `si commands` | List visible SI root commands | `list` | [CLI Reference](./CLI_REFERENCE) |

## Provider and integration command families

| Integration | Command family | Typical first checks | Detailed guide |
| --- | --- | --- | --- |
| GitHub | `si orbit github ...` | `si orbit github auth status`, `si orbit github doctor`, `si orbit github project list Aureuma` | [GitHub](./GITHUB) |
| Cloudflare | `si orbit cloudflare ...` | `si orbit cloudflare auth status`, `si orbit cloudflare doctor` | [Cloudflare](./CLOUDFLARE) |
| GCP + Gemini/Vertex | `si orbit gcp ...` | `si orbit gcp auth status`, `si orbit gcp doctor` | [GCP](./GCP) |
| Google Places | `si orbit google places ...` | `si orbit google places auth status`, `si orbit google places doctor` | [Google Places](./GOOGLE_PLACES) |
| Google Play | `si orbit google play ...` | `si orbit google play auth status`, `si orbit google play doctor` | [Google Play](./GOOGLE_PLAY) |
| YouTube Data | `si orbit google youtube ...` | `si orbit google youtube auth status`, `si orbit google youtube doctor` | [Google YouTube](./GOOGLE_YOUTUBE) |
| AWS | `si orbit aws ...` | `si orbit aws auth status`, `si orbit aws doctor` | [AWS](./AWS) |
| OpenAI | `si orbit openai ...` | `si orbit openai auth status`, `si orbit openai doctor` | [OpenAI](./OPENAI) |
| OCI | `si orbit oci ...` | `si orbit oci auth status`, `si orbit oci doctor` | [OCI](./OCI) |
| Stripe | `si orbit stripe ...` | `si orbit stripe auth status`, `si orbit stripe doctor` | [Stripe](./STRIPE) |
| WorkOS | `si orbit workos ...` | `si orbit workos auth status`, `si orbit workos doctor` | [WorkOS](./WORKOS) |
| Apple App Store Connect | `si orbit apple store ...` | `si orbit apple store auth status`, `doctor` | [Apple App Store](./APPLE_APPSTORE) |
| Provider inventory | `si orbit list` | `si orbit list`, `si orbit list --provider github --json` | [Providers](./PROVIDERS) |

## Build, docs, and developer tooling

| Command family | Purpose | Typical usage |
| --- | --- | --- |
| `si build self` | Build or upgrade `si` binary | `si build self` |
| `si build self check` | Fast typecheck for the SI CLI | `si build self check --timings` |
| `si build self assets` | Build all release archives + `checksums.txt` locally | `si build self assets --version vX.Y.Z` |
| `si commands` | Show visible public root commands | `si commands` |
| `si settings` | Inspect resolved settings | `si settings` |

## Recommended operator workflows

### 1. New machine bootstrap

```bash
si nucleus status
si nucleus service install
si nucleus service start
si build self
si build self check --timings
si vault status
si --help
si commands
```

### 2. Integration readiness check

```bash
si orbit list --json
si orbit github doctor --json
si orbit cloudflare doctor --json
```

### 3. Release maintainer preflight

```bash
si build self assets --version vX.Y.Z --out-dir .artifacts/release-preflight
si orbit github release create Aureuma/si --tag vX.Y.Z --title "vX.Y.Z" --target "$(git rev-parse HEAD)" --draft
```

- Use `--target <sha>` when creating a release for a tag that does not already exist on the remote.
- SI creates the tag ref first in that case; if `--target` is omitted, release creation fails clearly.

## Guardrails

- Use `si nucleus ...` rather than hidden runtime shortcuts when you need task, worker, session, run, or gateway state.
- Prefer `si nucleus service ...` over handwritten service units or launch agents.
- For host/admin automation, prefer `si vault run -- <cmd>` when a command needs secrets.
- For SI runtime workers, use `si fort ...` for secret access.
- `si fort` bootstrap/admin auth is file-backed and prefers explicit `--token-file` injection from `~/.si/fort/bootstrap/admin.token` when present.
- Pass native `fort` flags after `--` when invoking through wrapper.
- Run integration-specific `doctor` commands before write operations.
- Run `si help --format json` or `si commands` when updating CLI docs.
