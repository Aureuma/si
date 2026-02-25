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
- `codex.workspace` (string): host path for workspace bind.
  - If unset, `si spawn` resolves from `--workspace` or current directory.
  - On first interactive use, SI prompts to save the resolved path into `~/.si/settings.toml`.
- `codex.workdir` (string): container working directory
- `codex.repo` (string): default repo in `Org/Repo` form
- `codex.gh_pat` (string): optional PAT (stored in settings; keep file permissions restrictive)
- `codex.codex_volume` (string): override codex volume name
- `codex.skills_volume` (string): shared skills volume name (default: `si-codex-skills`)
- `codex.gh_volume` (string): override GitHub config volume name
- `codex.docker_socket` (bool): mount host Docker socket into codex containers (default: `true`)
- `codex.profile` (string): default profile ID/email
- `codex.detach` (bool): default detach behavior
- `codex.clean_slate` (bool): default clean-slate behavior

#### `[codex.login]`
Defaults for `si login`.
- `codex.login.device_auth` (bool): default device auth flow (`true`/`false`)
- `codex.login.open_url` (bool): open the login URL in a browser after it is printed
- `codex.login.open_url_command` (string): command to open the login URL. Use `{url}` to inject the URL, otherwise it is appended. Supported placeholders: `{url}`, `{profile}`, `{profile_id}`, `{profile_name}`, `{profile_email}`. Special values: `safari-profile` and `chrome-profile`.
- `codex.login.default_browser` (string): browser-aware launcher used when `open_url_command` is unset. Supported: `safari`, `chrome`. On macOS default is `safari`; elsewhere default is `chrome`.
Notes:
- In non-headless environments, `si login` detects one-time device codes and copies them to the clipboard (macOS: `pbcopy`, Linux: `wl-copy`, `xclip`, or `xsel`).
- In headless environments (for example Linux without `DISPLAY`/`WAYLAND_DISPLAY`), `si login` skips URL/device-code parsing and clipboard copy.
- macOS Safari profile windows (`open_url_command = "safari-profile"` or `default_browser = "safari"`) require Accessibility permission because SI uses AppleScript + `System Events` UI automation to click `File > New Window > <Profile>`.
  - Why: Safari does not provide a direct command-line/profile flag equivalent for opening a URL into a specific Safari profile window, so SI must automate the Safari menu.
  - If permission is not granted: SI warns clearly and falls back to opening Safari without profile selection.
  - Grant/review: `System Settings > Privacy & Security > Accessibility` and allow the app/terminal running `si`.

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
- `dyad.workspace` (string): host path for workspace bind.
  - If unset, `si dyad spawn` resolves from `--workspace` or current directory.
  - On first interactive use, SI prompts to save the resolved path into `~/.si/settings.toml`.
- `dyad.configs` (string): host path for configs
- `dyad.forward_ports` (string): port range, e.g. `1455-1465`
- `dyad.skills_volume` (string): shared skills volume name (default: `si-codex-skills`)
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
Defaults for `si github` (GitHub App or OAuth token auth).
- `github.default_account` (string): default account alias
- `github.default_auth_mode` (string): `app` or `oauth` (default: `app`)
- `github.api_base_url` (string): API base URL (default: `https://api.github.com`)
- `github.default_owner` (string): default owner/org for commands that accept owner fallback
- `github.vault_env` (string): vault env hint (default: `dev`)
- `github.vault_file` (string): optional explicit vault file path
- `github.log_file` (string): JSONL log path for GitHub bridge request/response events (default: `~/.si/logs/github.log`)

#### `[github.accounts.<alias>]`
Per-account GitHub settings.
- `name` (string): display name
- `owner` (string): default owner/org for this account
- `api_base_url` (string): per-account API base URL (supports GHES)
- `auth_mode` (string): `app` or `oauth` (overrides global default for this account)
- `vault_prefix` (string): env key prefix override (example `GITHUB_CORE_`)
- `oauth_access_token` (string): direct OAuth token (prefer env refs)
- `oauth_token_env` (string): env var with OAuth token
- `app_id` (int): direct app id (prefer env refs for secretless settings)
- `app_id_env` (string): env var with app id
- `app_private_key_pem` (string): direct private key PEM (prefer env refs)
- `app_private_key_env` (string): env var with private key PEM
- `installation_id` (int): explicit installation id
- `installation_id_env` (string): env var with installation id

Auth mode resolution for `si github`:
1. CLI override (`--auth-mode` where available)
2. Account settings (`auth_mode`)
3. Env fallback (`GITHUB_AUTH_MODE`, then `GITHUB_DEFAULT_AUTH_MODE`)
4. Global settings (`github.default_auth_mode`)

Credential resolution for `si github` in `app` mode:
1. CLI overrides (`--app-id`, `--app-key`, `--installation-id`)
2. Account settings (`app_id`, `app_private_key_pem`, `installation_id`)
3. Account env refs (`app_id_env`, `app_private_key_env`, `installation_id_env`)
4. Account-prefix env keys (`GITHUB_<ACCOUNT>_APP_ID`, `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`, `GITHUB_<ACCOUNT>_INSTALLATION_ID`)
5. Global env fallbacks (`GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PEM`, `GITHUB_INSTALLATION_ID`)

Credential resolution for `si github` in `oauth` mode:
1. CLI override (`--token` where available)
2. Account settings (`oauth_access_token`)
3. Account env ref (`oauth_token_env`)
4. Account-prefix env keys (`GITHUB_<ACCOUNT>_OAUTH_ACCESS_TOKEN`, `GITHUB_<ACCOUNT>_TOKEN`)
5. Global env fallbacks (`GITHUB_OAUTH_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`)

### `[cloudflare]`
Defaults for `si cloudflare` (token auth with multi-account and env context labels).
- `cloudflare.default_account` (string): default account alias
- `cloudflare.default_env` (string): `prod`, `staging`, or `dev` (default: `prod`)
- `cloudflare.api_base_url` (string): API base URL (default: `https://api.cloudflare.com/client/v4`)
- `cloudflare.vault_env` (string): vault env hint (default: `dev`)
- `cloudflare.vault_file` (string): optional explicit vault file path
- `cloudflare.log_file` (string): JSONL log path for Cloudflare bridge request/response events (default: `~/.si/logs/cloudflare.log`)

#### `[cloudflare.accounts.<alias>]`
Per-account Cloudflare context and env-key pointers.
- `name` (string): display name
- `account_id` (string): Cloudflare account id
- `account_id_env` (string): env var with account id
- `api_base_url` (string): per-account API base URL override
- `vault_prefix` (string): env key prefix override (example `CLOUDFLARE_CORE_`)
- `default_zone_id` (string): default zone id fallback
- `default_zone_name` (string): default zone name fallback
- `prod_zone_id` (string): zone id used when `env=prod`
- `staging_zone_id` (string): zone id used when `env=staging`
- `dev_zone_id` (string): zone id used when `env=dev`
- `api_token_env` (string): env var with API token

Credential resolution for `si cloudflare` is vault-compatible and token-only:
1. CLI overrides (`--api-token`, `--account-id`, `--zone-id`)
2. Account settings (`account_id`, env-mapped zone ids, defaults)
3. Account env refs (`account_id_env`, `api_token_env`)
4. Account-prefix env keys (`CLOUDFLARE_<ACCOUNT>_API_TOKEN`, `CLOUDFLARE_<ACCOUNT>_ACCOUNT_ID`, `CLOUDFLARE_<ACCOUNT>_PROD_ZONE_ID`, `CLOUDFLARE_<ACCOUNT>_STAGING_ZONE_ID`, `CLOUDFLARE_<ACCOUNT>_DEV_ZONE_ID`)
5. Global env fallbacks (`CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`)

### `[gcp]`
Defaults for `si gcp` (Service Usage, IAM, API keys, Gemini, and Vertex AI).
- `gcp.default_account` (string): default account alias
- `gcp.default_env` (string): `prod`, `staging`, or `dev` (default: `prod`)
- `gcp.api_base_url` (string): default API base URL used by `si gcp service` (default: `https://serviceusage.googleapis.com`)
- `gcp.log_file` (string): JSONL log path for GCP bridge events (default: `~/.si/logs/gcp-serviceusage.log`)

#### `[gcp.accounts.<alias>]`
Per-account GCP context and env-key pointers.
- `name` (string): display name
- `vault_prefix` (string): env key prefix override (example `GCP_CORE_`)
- `project_id` (string): default Google Cloud project id
- `project_id_env` (string): env var with project id
- `access_token_env` (string): env var with OAuth access token
- `api_key_env` (string): env var with API key (used by Gemini API-key mode)
- `api_base_url` (string): per-account API base URL override

Credential resolution for `si gcp` project id:
1. CLI override (`--project`)
2. Account settings (`project_id`)
3. Account env ref (`project_id_env`)
4. Account-prefix env key (`GCP_<ACCOUNT>_PROJECT_ID`)
5. Global env fallbacks (`GCP_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`)

Credential resolution for `si gcp` OAuth token:
1. CLI override (`--access-token`)
2. Account env ref (`access_token_env`)
3. Account-prefix env key (`GCP_<ACCOUNT>_ACCESS_TOKEN`)
4. Global env fallbacks (`GOOGLE_OAUTH_ACCESS_TOKEN`, `GCP_ACCESS_TOKEN`)

Credential resolution for Gemini API-key mode (`si gcp gemini`):
1. CLI override (`--api-key`)
2. Account env ref (`api_key_env`)
3. Account-prefix env key (`GCP_<ACCOUNT>_API_KEY`)
4. Global env fallbacks (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GCP_API_KEY`)

### `[google]`
Defaults for `si google places` and `si google youtube` (multi-account and env context labels).
- `google.default_account` (string): default account alias
- `google.default_env` (string): `prod`, `staging`, or `dev` (default: `prod`)
- `google.api_base_url` (string): API base URL (default: `https://places.googleapis.com`)
- `google.vault_env` (string): vault env hint (default: `dev`)
- `google.vault_file` (string): optional explicit vault file path
- `google.log_file` (string): shared JSONL log path override for Google bridges. If unset, Places defaults to `~/.si/logs/google-places.log` and YouTube defaults to `~/.si/logs/google-youtube.log`.

#### `[google.accounts.<alias>]`
Per-account Google Places context and env-key pointers.
- `name` (string): display name
- `project_id` (string): default Google Cloud project id
- `project_id_env` (string): env var with project id
- `api_base_url` (string): per-account API base URL override
- `vault_prefix` (string): env key prefix override (example `GOOGLE_CORE_`)
- `places_api_key_env` (string): env var with generic Places API key
- `prod_places_api_key_env` (string): env var with prod Places API key
- `staging_places_api_key_env` (string): env var with staging Places API key
- `dev_places_api_key_env` (string): env var with dev Places API key
- `default_region_code` (string): default CLDR region code
- `default_language_code` (string): default BCP-47 language code

Credential resolution for `si google places` is vault-compatible and API-key based:
1. CLI overrides (`--api-key`, `--project-id`)
2. Account settings (`project_id`)
3. Account env refs (`project_id_env`, `places_api_key_env`, `prod_places_api_key_env`, `staging_places_api_key_env`, `dev_places_api_key_env`)
4. Account-prefix env keys (`GOOGLE_<ACCOUNT>_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_PROD_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_STAGING_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_DEV_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_PROJECT_ID`)
5. Global env fallbacks (`GOOGLE_PLACES_API_KEY`, `GOOGLE_PROJECT_ID`)

### `[google.youtube]`
Defaults for `si google youtube` (YouTube Data API v3).
- `google.youtube.api_base_url` (string): API base URL (default: `https://www.googleapis.com`)
- `google.youtube.upload_base_url` (string): upload API base URL (default: `https://www.googleapis.com/upload`)
- `google.youtube.default_auth_mode` (string): `api-key` or `oauth` (default: `api-key`)
- `google.youtube.upload_chunk_size_mb` (int): default chunk hint for upload flows (default: `16`)

#### `[google.youtube.accounts.<alias>]`
Per-account YouTube context and env-key pointers.
- `name` (string): display name
- `project_id` (string): default Google Cloud project id
- `project_id_env` (string): env var with project id
- `vault_prefix` (string): env key prefix override (example `GOOGLE_CORE_`)
- `youtube_api_key_env` (string): env var with generic YouTube API key
- `prod_youtube_api_key_env` (string): env var with prod YouTube API key
- `staging_youtube_api_key_env` (string): env var with staging YouTube API key
- `dev_youtube_api_key_env` (string): env var with dev YouTube API key
- `youtube_client_id_env` (string): env var with OAuth client id
- `youtube_client_secret_env` (string): env var with OAuth client secret
- `youtube_redirect_uri_env` (string): env var with OAuth redirect uri
- `youtube_refresh_token_env` (string): env var with OAuth refresh token
- `default_region_code` (string): default region code
- `default_language_code` (string): default language code

Credential resolution for `si google youtube` is vault-compatible and supports both API key and OAuth:
1. CLI overrides (`--api-key`, `--project-id`, `--client-id`, `--client-secret`, `--redirect-uri`, `--access-token`, `--refresh-token`)
2. Account settings (`project_id`)
3. Account env refs (`project_id_env`, `youtube_api_key_env`, env-specific api key refs, OAuth refs)
4. Account-prefix env keys (`GOOGLE_<ACCOUNT>_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_ID`, `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_SECRET`, `GOOGLE_<ACCOUNT>_YOUTUBE_REDIRECT_URI`, `GOOGLE_<ACCOUNT>_YOUTUBE_ACCESS_TOKEN`, `GOOGLE_<ACCOUNT>_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_REFRESH_TOKEN`)
5. Global env fallbacks (`GOOGLE_YOUTUBE_API_KEY`, `GOOGLE_YOUTUBE_CLIENT_ID`, `GOOGLE_YOUTUBE_CLIENT_SECRET`, `GOOGLE_YOUTUBE_REDIRECT_URI`, `GOOGLE_YOUTUBE_ACCESS_TOKEN`, `GOOGLE_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_PROJECT_ID`)

Local OAuth token cache for `si google youtube auth login` is stored at:
- `~/.si/google/youtube/oauth_tokens.json`

### `[vault]`
Defaults for `si vault` (Sun remote vault).
- `vault.file` (string): default **scope** used when `--scope`/`--file` is not provided (default: `default`)
- `vault.trust_store` (string): deprecated in Sun mode (`trust: n/a (sun-managed)`)
- `vault.audit_log` (string): optional local JSONL audit sink (empty by default)
- `vault.key_backend` (string): deprecated compatibility field in Sun mode
- `vault.key_file` (string): deprecated compatibility field in Sun mode
- `vault.sync_backend` (string): backend mode. `sun` is the effective/only mode. Legacy aliases (`git`, `local`, `dual`, `both`, `cloud`) normalize to `sun`.

### `[sun]`
Defaults for `si sun` cloud sync.
- `sun.base_url` (string): Sun API base URL (default: `http://127.0.0.1:8080`)
- `sun.account` (string): expected account slug bound to the saved token
- `sun.token` (string): Sun bearer token
- `sun.timeout_seconds` (int): request timeout for Sun API calls (default: `15`)
- `sun.auto_sync` (bool): enable automatic codex profile sync hooks
- `sun.plugin_gateway_registry` (string): default remote integration registry for `si plugins gateway ...` (default: `global`)
- `sun.plugin_gateway_slots` (int): default slots-per-namespace for gateway build/push partitioning (default: `16`, max: `256`)
- `sun.taskboard` (string): default Sun object name for shared dyad taskboard (default: `default`)
- `sun.taskboard_agent` (string): optional default agent id used by `si sun taskboard` and dyad autopilot claims
- `sun.taskboard_lease_seconds` (int): default assignment lease duration in seconds (default: `1800`)
- `sun.machine_id` (string): default machine id for `si sun machine ...` commands
- `sun.operator_id` (string): default operator identity for machine ACL and dispatch actions

Environment overrides:
- `SI_SUN_BASE_URL`
- `SI_SUN_TOKEN`
- `SI_SUN_LOGIN_URL`
- `SI_SUN_LOGIN_OPEN_CMD`
- `SI_SUN_ALLOW_INSECURE_HTTP`
- `SI_SUN_PLUGIN_GATEWAY_REGISTRY`
- `SI_SUN_PLUGIN_GATEWAY_SLOTS`
- `SI_SUN_TASKBOARD`
- `SI_SUN_TASKBOARD_AGENT`
- `SI_SUN_TASKBOARD_LEASE_SECONDS`
- `SI_SUN_MACHINE_ID`
- `SI_SUN_OPERATOR_ID`
- `SI_VAULT_SYNC_BACKEND`

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
workspace = "/path/to/your/repo"
workdir = "/workspace"
docker_socket = true
profile = "america"
detach = true
clean_slate = false

[codex.login]
device_auth = true
open_url = false
default_browser = "safari"

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
workspace = "/path/to/your/repo"

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
auth_mode = "app"
app_id_env = "GITHUB_CORE_APP_ID"
app_private_key_env = "GITHUB_CORE_APP_PRIVATE_KEY_PEM"
installation_id_env = "GITHUB_CORE_INSTALLATION_ID"
oauth_token_env = "GITHUB_CORE_OAUTH_ACCESS_TOKEN"

[cloudflare]
default_account = "core"
default_env = "prod"
api_base_url = "https://api.cloudflare.com/client/v4"
log_file = "~/.si/logs/cloudflare.log"

[cloudflare.accounts.core]
name = "Core Cloudflare"
vault_prefix = "CLOUDFLARE_CORE_"
account_id_env = "CLOUDFLARE_CORE_ACCOUNT_ID"
api_token_env = "CLOUDFLARE_CORE_API_TOKEN"
prod_zone_id = "11111111111111111111111111111111"
staging_zone_id = "22222222222222222222222222222222"
dev_zone_id = "33333333333333333333333333333333"

[google]
default_account = "core"
default_env = "prod"
api_base_url = "https://places.googleapis.com"
log_file = "~/.si/logs/google-places.log"

[google.accounts.core]
name = "Core Google Places"
project_id = "acme-places-prod"
vault_prefix = "GOOGLE_CORE_"
places_api_key_env = "GOOGLE_CORE_PLACES_API_KEY"
prod_places_api_key_env = "GOOGLE_CORE_PROD_PLACES_API_KEY"
staging_places_api_key_env = "GOOGLE_CORE_STAGING_PLACES_API_KEY"
dev_places_api_key_env = "GOOGLE_CORE_DEV_PLACES_API_KEY"
default_region_code = "US"
default_language_code = "en"

[google.youtube]
api_base_url = "https://www.googleapis.com"
upload_base_url = "https://www.googleapis.com/upload"
default_auth_mode = "api-key"
upload_chunk_size_mb = 16

[google.youtube.accounts.core]
name = "Core YouTube"
project_id = "acme-youtube-prod"
vault_prefix = "GOOGLE_CORE_"
youtube_api_key_env = "GOOGLE_CORE_YOUTUBE_API_KEY"
prod_youtube_api_key_env = "GOOGLE_CORE_PROD_YOUTUBE_API_KEY"
staging_youtube_api_key_env = "GOOGLE_CORE_STAGING_YOUTUBE_API_KEY"
dev_youtube_api_key_env = "GOOGLE_CORE_DEV_YOUTUBE_API_KEY"
youtube_client_id_env = "GOOGLE_CORE_YOUTUBE_CLIENT_ID"
youtube_client_secret_env = "GOOGLE_CORE_YOUTUBE_CLIENT_SECRET"
youtube_redirect_uri_env = "GOOGLE_CORE_YOUTUBE_REDIRECT_URI"
youtube_refresh_token_env = "GOOGLE_CORE_YOUTUBE_REFRESH_TOKEN"
default_region_code = "US"
default_language_code = "en"

[social]
default_account = "core"
default_env = "prod"
log_file = "~/.si/logs/social.log"

[social.facebook]
api_base_url = "https://graph.facebook.com"
api_version = "v22.0"
auth_style = "query"

[social.instagram]
api_base_url = "https://graph.facebook.com"
api_version = "v22.0"
auth_style = "query"

[social.x]
api_base_url = "https://api.twitter.com"
api_version = "2"
auth_style = "bearer"

[social.linkedin]
api_base_url = "https://api.linkedin.com"
api_version = "v2"
auth_style = "bearer"

[social.reddit]
api_base_url = "https://oauth.reddit.com"
auth_style = "bearer"

[social.accounts.core]
name = "Core Social"
vault_prefix = "SOCIAL_CORE_"
facebook_access_token_env = "SOCIAL_CORE_FACEBOOK_ACCESS_TOKEN"
instagram_access_token_env = "SOCIAL_CORE_INSTAGRAM_ACCESS_TOKEN"
x_access_token_env = "SOCIAL_CORE_X_BEARER_TOKEN"
linkedin_access_token_env = "SOCIAL_CORE_LINKEDIN_ACCESS_TOKEN"
reddit_access_token_env = "SOCIAL_CORE_REDDIT_ACCESS_TOKEN"
facebook_page_id = "1234567890"
instagram_business_id = "17890000000000000"
x_username = "acme"
linkedin_person_urn = "urn:li:person:abc123"
reddit_username = "acme_bot"

[vault]
file = "default"
trust_store = ""
audit_log = ""
key_backend = "keyring" # compatibility only in Sun mode
key_file = ""           # compatibility only in Sun mode
sync_backend = "sun"

[sun]
base_url = "http://127.0.0.1:8080"
account = "acme"
timeout_seconds = 15
auto_sync = true
plugin_gateway_registry = "global"
plugin_gateway_slots = 16
taskboard = "default"
taskboard_agent = "dyad:main-laptop"
taskboard_lease_seconds = 1800
machine_id = "laptop-a"
operator_id = "op:shawn@laptop-a"

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
