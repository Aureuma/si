# Settings Reference (`~/.si/settings.toml`)

`si` reads a single TOML file for user-facing configuration. The canonical path is:

```
~/.si/settings.toml
```

This file is created automatically on first use. It is also updated when you log in with `si login` so that profile metadata (auth path/timestamps) are tracked in one place.

## Precedence
When supported by a command, values resolve in this order:

1. CLI flags
2. `~/.si/settings.toml`
3. Environment variables
4. Built-in defaults

## Schema
The file is a standard TOML document. `schema_version` is reserved for future migrations.

### Top-level
- `schema_version` (int): settings schema version. Current value: `1`.

### `[paths]`
Reference paths for the local `.si` directory layout.
- `paths.root` (string): default `~/.si`
- `paths.settings` (string): default `~/.si/settings.toml`
- `paths.codex_profiles_dir` (string): default `~/.si/codex/profiles`

Warmup runtime files are also stored under `~/.si`:
- `~/.si/warmup/state.json` (reconcile state/feedback loop)
- `~/.si/warmup/autostart.v1` (warmup scheduler enabled marker)
- `~/.si/warmup/disabled.v1` (warmup scheduler disabled marker)
- `~/.si/logs/warmup.log` (JSONL operational log)

Warmup scheduling is triggered by `si login` (and explicit `si warmup enable`), not by `si status`.

### `[codex]`
Defaults for Codex container commands (spawn/respawn/login/run).
- `codex.image` (string): docker image for `si spawn` (default: `aureuma/si:local`)
- `codex.network` (string): docker network name
- `codex.workspace` (string): host path for workspace bind
- `codex.workdir` (string): container working directory
- `codex.repo` (string): default repo in `Org/Repo` form
- `codex.gh_pat` (string): optional PAT (stored in settings; keep file permissions restrictive)
- `codex.codex_volume` (string): override codex volume name
- `codex.gh_volume` (string): override GitHub config volume name
- `codex.docker_socket` (bool): mount host Docker socket into codex containers (default: `true`)
- `codex.profile` (string): default profile ID/email
- `codex.detach` (bool): default detach behavior
- `codex.clean_slate` (bool): default clean-slate behavior

#### `[codex.login]`
Defaults for `si login`.
- `codex.login.device_auth` (bool): default device auth flow (`true`/`false`)
- `codex.login.open_url` (bool): open the login URL in a browser after it is printed
- `codex.login.open_url_command` (string): command to open the login URL. Use `{url}` to inject the URL, otherwise it is appended. Supported placeholders: `{url}`, `{profile}`, `{profile_id}`, `{profile_name}`, `{profile_email}`. Special value `safari-profile` opens Safari using a profile window derived from the selected Codex profile name (including emojis). macOS only; requires Accessibility permission for System Events. Use `si login --safari-profile "<name>"` to override.
Notes:
- When `si login` detects a one-time device code, it copies it to the clipboard (macOS: `pbcopy`, Linux: `wl-copy`, `xclip`, or `xsel`).

#### `[codex.exec]`
Defaults for one-off `si run` (alias `si exec`).
- `codex.exec.model` (string): default model
- `codex.exec.effort` (string): default reasoning effort

#### `[codex.profiles]`
Profile metadata tracked in settings.
- `codex.profiles.active` (string): the last profile used for login

##### `[codex.profiles.entries.<id>]`
Per-profile entry keyed by profile ID (for example `america`). These entries are updated on successful `si login`.
- `name` (string): profile display name
- `email` (string): profile email
- `auth_path` (string): path to auth.json
- `auth_updated` (string): RFC3339 timestamp of auth.json

### `[dyad]`
Defaults for dyad spawns.
- `dyad.actor_image` (string): default `aureuma/si:local`
- `dyad.critic_image` (string): default `aureuma/si:local`
- `dyad.codex_model` (string)
- `dyad.codex_effort_actor` (string)
- `dyad.codex_effort_critic` (string)
- `dyad.codex_model_low` (string)
- `dyad.codex_model_medium` (string)
- `dyad.codex_model_high` (string)
- `dyad.codex_effort_low` (string)
- `dyad.codex_effort_medium` (string)
- `dyad.codex_effort_high` (string)
- `dyad.workspace` (string): host path for workspace bind
- `dyad.configs` (string): host path for configs
- `dyad.forward_ports` (string): port range, e.g. `1455-1465`
- `dyad.docker_socket` (bool): mount host Docker socket into dyad containers (default: `true`)

### `[stripe]`
Defaults for `si stripe` account and environment context.
- `stripe.organization` (string): optional organization label
- `stripe.default_account` (string): default account alias (or `acct_` id)
- `stripe.default_env` (string): `live` or `sandbox` (default: `sandbox`)
- `stripe.log_file` (string): JSONL log path for Stripe bridge request/response events (default: `~/.si/logs/stripe.log`)

#### `[stripe.accounts.<alias>]`
Per-account Stripe settings.
- `id` (string): Stripe account id (`acct_...`) used for scoped calls
- `name` (string): display name
- `live_key` (string): direct live API key (prefer env refs instead)
- `sandbox_key` (string): direct sandbox API key (prefer env refs instead)
- `live_key_env` (string): env var name holding the live key
- `sandbox_key_env` (string): env var name holding the sandbox key

Credential resolution order for `si stripe`:
1. `--api-key` (or `--live-api-key`/`--sandbox-api-key` for sync)
2. Account settings key (`live_key` / `sandbox_key`)
3. Account settings env ref (`live_key_env` / `sandbox_key_env`)
4. Environment-specific env fallback (`SI_STRIPE_LIVE_API_KEY` / `SI_STRIPE_SANDBOX_API_KEY`)
5. Generic env fallback (`SI_STRIPE_API_KEY`)

`SI_STRIPE_ACCOUNT` can provide default account selection when settings do not specify one.

### `[github]`
Defaults for `si github` (GitHub App-only auth).
- `github.default_account` (string): default account alias
- `github.default_auth_mode` (string): always `app` for `si github`
- `github.api_base_url` (string): API base URL (default: `https://api.github.com`)
- `github.default_owner` (string): default owner/org for commands that accept owner fallback
- `github.vault_env` (string): vault env hint (default: `dev`)
- `github.vault_file` (string): optional explicit vault file path
- `github.log_file` (string): JSONL log path for GitHub bridge request/response events (default: `~/.si/logs/github.log`)

#### `[github.accounts.<alias>]`
Per-account GitHub App settings.
- `name` (string): display name
- `owner` (string): default owner/org for this account
- `api_base_url` (string): per-account API base URL (supports GHES)
- `auth_mode` (string): kept for compatibility; `si github` enforces `app`
- `vault_prefix` (string): env key prefix override (example `GITHUB_CORE_`)
- `app_id` (int): direct app id (prefer env refs for secretless settings)
- `app_id_env` (string): env var with app id
- `app_private_key_pem` (string): direct private key PEM (prefer env refs)
- `app_private_key_env` (string): env var with private key PEM
- `installation_id` (int): explicit installation id
- `installation_id_env` (string): env var with installation id

Credential resolution for `si github` is vault-compatible and app-only:
1. CLI overrides (`--app-id`, `--app-key`, `--installation-id`)
2. Account settings (`app_id`, `app_private_key_pem`, `installation_id`)
3. Account env refs (`app_id_env`, `app_private_key_env`, `installation_id_env`)
4. Account-prefix env keys (`GITHUB_<ACCOUNT>_APP_ID`, `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`, `GITHUB_<ACCOUNT>_INSTALLATION_ID`)
5. Global env fallbacks (`GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PEM`, `GITHUB_INSTALLATION_ID`)

### `[vault]`
Defaults for `si vault` (encrypted `.env.<env>` files, typically stored in a separate vault repo submodule).
- `vault.dir` (string): vault directory relative to the current host repo root (default: `vault`)
- `vault.default_env` (string): default environment name used to resolve `.env.<env>` (default: `dev`)
- `vault.trust_store` (string): local TOFU trust store path (default: `~/.si/vault/trust.json`)
- `vault.audit_log` (string): JSONL audit log path (default: `~/.si/logs/vault.log`)
- `vault.key_backend` (string): where the device private key is stored. Supported: `keyring`, `file` (default: `keyring`)
- `vault.key_file` (string): identity file path used when `vault.key_backend = "file"` (default: `~/.si/vault/keys/age.key`)

### `[shell.prompt]`
Prompt rendering for `si run` interactive shells. This applies without modifying `.bashrc`.
- `shell.prompt.enabled` (bool): enable/disable prompt customization
- `shell.prompt.git_enabled` (bool): include git branch when available
- `shell.prompt.prefix_template` (string): template for profile prefix. Use `{profile}` placeholder.
- `shell.prompt.format` (string): layout template. Supported placeholders: `{prefix}`, `{cwd}`, `{git}`, `{symbol}`
- `shell.prompt.symbol` (string): prompt symbol (e.g. `$` or `❯`)

#### `[shell.prompt.colors]`
Color tokens for prompt components. Supported values:
- Basic names: `black`, `red`, `green`, `yellow`, `blue`, `magenta`, `cyan`, `white`
- Bright variants: `bright-black`, `bright-red`, `bright-green`, `bright-yellow`, `bright-blue`, `bright-magenta`, `bright-cyan`, `bright-white`
- `reset`
- `ansi:<code>` where `<code>` is an ANSI numeric color code (e.g. `ansi:0;36`)
- `raw:<value>` to pass a raw escape sequence

Keys:
- `shell.prompt.colors.profile`
- `shell.prompt.colors.cwd`
- `shell.prompt.colors.git`
- `shell.prompt.colors.symbol`
- `shell.prompt.colors.error`
- `shell.prompt.colors.reset`

## Example
```toml
schema_version = 1

[paths]
root = "~/.si"
settings = "~/.si/settings.toml"
codex_profiles_dir = "~/.si/codex/profiles"

[codex]
image = "aureuma/si:local"
network = "si"
workspace = "/home/ubuntu/Development/si"
workdir = "/workspace"
docker_socket = true
profile = "america"
detach = true
clean_slate = false

[codex.login]
device_auth = true
open_url = false
open_url_command = "safari-profile"

[codex.exec]
model = "gpt-5.2-codex"
effort = "medium"

[codex.profiles]
active = "america"

[codex.profiles.entries.america]
name = "🗽 America"
email = "example@example.com"
auth_path = "~/.si/codex/profiles/america/auth.json"
auth_updated = "2026-01-26T00:00:00Z"

[dyad]
actor_image = "aureuma/si:local"
critic_image = "aureuma/si:local"
codex_model = "gpt-5.2-codex"
forward_ports = "1455-1465"
docker_socket = true
workspace = "/home/ubuntu/Development/si"

[stripe]
organization = "main-org"
default_account = "core"
default_env = "sandbox"
log_file = "~/.si/logs/stripe.log"

[stripe.accounts.core]
id = "acct_1234567890"
name = "Core Account"
live_key_env = "STRIPE_CORE_LIVE_KEY"
sandbox_key_env = "STRIPE_CORE_SANDBOX_KEY"

[github]
default_account = "core"
default_auth_mode = "app"
api_base_url = "https://api.github.com"
default_owner = "Aureuma"
log_file = "~/.si/logs/github.log"

[github.accounts.core]
name = "Core GitHub App"
owner = "Aureuma"
vault_prefix = "GITHUB_CORE_"
app_id_env = "GITHUB_CORE_APP_ID"
app_private_key_env = "GITHUB_CORE_APP_PRIVATE_KEY_PEM"
installation_id_env = "GITHUB_CORE_INSTALLATION_ID"

[vault]
dir = "vault"
default_env = "dev"
trust_store = "~/.si/vault/trust.json"
audit_log = "~/.si/logs/vault.log"
key_backend = "keyring"
key_file = "~/.si/vault/keys/age.key"

[shell.prompt]
enabled = true
git_enabled = true
prefix_template = "[{profile}] "
format = "{prefix}{cwd}{git} {symbol} "
symbol = "$"

[shell.prompt.colors]
profile = "cyan"
cwd = "blue"
git = "magenta"
symbol = "green"
error = "red"
reset = "reset"
```
