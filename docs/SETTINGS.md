# Settings Reference (`~/.si/settings.toml`)

`si` reads a single TOML file for user-facing configuration. The canonical path is:

```
~/.si/settings.toml
```

This file is created automatically on first use. `si codex profile ...` and `si muse profile ...` write profile metadata here so profile registry state and default runtime selection share one source of truth.

## Precedence
When supported by a command, values resolve in this order:

1. CLI flags
2. `~/.si/settings.toml`
3. Environment variables
4. Built-in defaults

## CLI color output

SI CLI runtime text output shares one semantic color palette:

- commands/examples: magenta
- labels: blue

Clap-rendered `--help` output keeps its own styled headers, usage lines, and literals.

Color control is environment-driven:

- `SI_CLI_COLOR=always`: force color
- `SI_CLI_COLOR=auto`: default behavior
- `SI_CLI_COLOR=never`: disable color
- `NO_COLOR=1`: disable color

Structured JSON output remains uncolored.

## Schema
The file is a standard TOML document. `schema_version` is reserved for future migrations.

### Top-level
- `schema_version` (int): settings schema version. Current value: `1`.

### `[paths]`
Reference paths for the local `.si` directory layout.
- `paths.root` (string): default `~/.si`
- `paths.settings_file` (string): default `~/.si/settings.toml`
- `paths.codex_profiles_dir` (string): default `~/.si/codex/profiles`
- `paths.workspace_root` (string): optional host directory containing sibling repos. Used by SI runtime commands when flags are omitted.

### `[codex]`
Defaults for `si codex` profile-bound worker commands.
- Every `si codex` worker must resolve to a predefined entry under `[codex.profiles.entries.<id>]`.
  - `si codex profile add|show|list|login|logout|select|remove` manages the profile registry in `~/.si/settings.toml`.
- `codex.workspace` (string): host path for workspace bind.
  - If unset, `si codex spawn` resolves from `--workspace` or current directory.
  - On first interactive use, SI prompts to save the resolved path into `~/.si/settings.toml`.
- `codex.workdir` (string): worker working directory
- `codex.profile` (string): legacy compatibility field for the most recently selected Codex profile.
- Profile metadata is intentionally narrow here: the entry records identity and auth file location, while actual runtime behavior stays under `si codex ...`.
- Worker-slot behavior is command-level:
  - `si codex spawn|respawn --profile <profile> --slot <slot>`
  - `si codex shell|tail|tmux|stop|remove --profile <profile> --slot <slot>`

#### `[codex.profiles]`
Profile metadata tracked in settings.
- `codex.profiles.active` (string): the most recently selected profile for profile-scoped commands and related host state

##### `[codex.profiles.entries.<id>]`
Per-profile entry keyed by profile ID (for example `profile-alpha`). These entries are created and updated by `si codex profile add` and any later profile metadata sync flows.
- `name` (string): profile display name
- `email` (string): profile email
- `auth_path` (string): path to auth.json
- `auth_updated` (string): RFC3339 timestamp of auth.json
- `native_root` (string): canonical native home for this account
- `identity` (string): short identity summary bound at enrollment
- `identity_source` (string): how `identity` was verified, or `identity_unverified`
- `enrolled_at` (string): RFC3339 enrollment timestamp
- `disabled` (string): set with a reason when launch is disabled for this profile

### `[muse]`
Defaults for `si muse` profile-bound worker commands. Mirrors `[codex]`.
- Every `si muse` worker must resolve to a predefined entry under `[muse.profiles.entries.<id>]`.
  - `si muse profile add|show|list|login|logout|select|remove` manages the profile registry in `~/.si/settings.toml`.
- `muse.workspace` (string): host path for workspace bind.
- `muse.workdir` (string): worker working directory
- `muse.profile` (string): legacy compatibility field for the most recently selected Muse profile.
- Worker-slot behavior is command-level:
  - `si muse spawn|respawn --profile <profile> --slot <slot>`
  - `si muse shell|tail|tmux|stop|remove --profile <profile> --slot <slot>`

#### `[muse.profiles]`
Profile metadata tracked in settings.
- `muse.profiles.active` (string): the most recently selected profile for profile-scoped commands and related host state

##### `[muse.profiles.entries.<id>]`
Per-profile entry keyed by profile ID. These entries are created and updated by `si muse profile add` and any later profile metadata sync flows.
- `name` (string): profile display name
- `email` (string): profile email
- `native_root` (string): canonical native root for this account (holds config/data/cache/state)
- `identity` (string): short identity summary bound at enrollment
- `identity_source` (string): how `identity` was verified, or `identity_unverified`
- `enrolled_at` (string): RFC3339 enrollment timestamp
- `disabled` (string): set with a reason when launch is disabled for this profile

### External orbit settings
Third-party integration settings moved to the standalone `orbit` repo. SI no longer reads provider account settings such as `[stripe]`, `[github]`, `[cloudflare]`, `[gcp]`, `[google]`, `[openai]`, `[oci]`, `[apple]`, or `[workos]`.
## Example
```toml
schema_version = 1

[paths]
root = "~/.si"
settings = "~/.si/settings.toml"
codex_profiles_dir = "~/.si/codex/profiles"
workspace_root = "~/code"

[codex]
workspace = "/path/to/your/repo"
workdir = "/workspace"
profile = "profile-alpha"

[codex.profiles]
active = "profile-alpha"

[codex.profiles.entries.profile-alpha]
name = "🧪 Profile Alpha"
email = "example@example.com"
auth_path = "~/.si/codex/profiles/profile-alpha/auth.json"
auth_updated = "2026-01-26T00:00:00Z"
