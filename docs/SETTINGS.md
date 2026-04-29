# Settings Reference (`~/.si/settings.toml`)

`si` reads a single TOML file for user-facing configuration. The canonical path is:

```
~/.si/settings.toml
```

This file is created automatically on first use. `si codex profile ...` writes Codex profile metadata here so profile registry state, Fort profile binding, and default runtime selection all share one source of truth.

## Precedence
When supported by a command, values resolve in this order:

1. CLI flags
2. `~/.si/settings.toml`
3. Environment variables
4. Built-in defaults

## CLI color output

SI CLI help and text-mode output share one semantic color palette:

- section headings: cyan
- commands/examples: magenta
- flags/prompts: yellow
- labels: blue
- success: green
- warnings: yellow
- errors: red
- muted text: gray

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
- `paths.workspace_root` (string): optional host directory containing sibling repos. Used by commands such as `si orbit github git ...` and `si remote-control` when flags are omitted.

Warmup runtime files are also stored under `~/.si`:
- `~/.si/warmup/state.json` (reconcile state/feedback loop)
- `~/.si/warmup/autostart.v1` (warmup scheduler enabled marker)
- `~/.si/warmup/disabled.v1` (warmup scheduler disabled marker)
- `~/.si/logs/warmup.log` (JSONL operational log)

Warmup scheduling is auto-enabled once SI sees cached codex profile auth on disk, and it can also be controlled explicitly with `si codex warmup ...`.
Warmup only inspects persistent Codex worker status and schedules the next run from the reported reset windows with a small jitter.
Warmup only reports a profile as warmed once the live weekly quota drops below `100%` left.

## Nucleus gateway discovery and auth

`si nucleus ...` does not currently use a dedicated `[nucleus]` table in `~/.si/settings.toml`.
Its local gateway discovery and auth contract is environment- and metadata-based instead:

1. `--endpoint`
2. `SI_NUCLEUS_WS_ADDR`
3. `~/.si/nucleus/gateway/metadata.json`
4. default `ws://127.0.0.1:4747/ws`

Additional env vars:

- `SI_NUCLEUS_AUTH_TOKEN`: bearer token forwarded by CLI gateway clients when set
- `SI_NUCLEUS_STATE_DIR`: override the Nucleus state root for service/runtime commands
- `SI_NUCLEUS_BIND_ADDR`: override the Nucleus bind address for service/runtime commands
- `SI_NUCLEUS_SERVICE_PLATFORM`: force service generation to `systemd-user` or `launchd-agent`

The metadata file is written by the Nucleus service and advertises the current websocket URL and SI version for local CLI bootstrap.

### `[codex]`
Defaults for `si codex` profile-bound worker commands.
- Every `si codex` worker must resolve to a predefined entry under `[codex.profiles.entries.<id>]`.
- `si codex profile add|show|list|login|swap|remove` manages the profile registry in `~/.si/settings.toml`.
- `codex.workspace` (string): host path for workspace bind.
  - If unset, `si codex spawn` resolves from `--workspace` or current directory.
  - On first interactive use, SI prompts to save the resolved path into `~/.si/settings.toml`.
- `codex.workdir` (string): worker working directory
- `codex.profile` (string): legacy compatibility field for the most recently selected Codex profile.
- Profile metadata is intentionally narrow here: the entry records identity and auth file location, while actual runtime behavior stays under `si codex ...`.

#### `[codex.profiles]`
Profile metadata tracked in settings.
- `codex.profiles.active` (string): the most recently swapped/selected profile for profile-scoped Fort runtime auth and related host state

##### `[codex.profiles.entries.<id>]`
Per-profile entry keyed by profile ID (for example `profile-alpha`). These entries are created and updated by `si codex profile add` and any later profile metadata sync flows.
- `name` (string): profile display name
- `email` (string): profile email
- `auth_path` (string): path to auth.json
- `auth_updated` (string): RFC3339 timestamp of auth.json

### `[fort]`
Defaults for the `si fort` wrapper (hosted Fort API access).
- `fort.repo` (string): source repo path used when `si fort --build` is enabled
- `fort.bin` (string): fort binary path used by wrapper execution
- `fort.build` (bool): default build-before-run behavior for wrapper calls
- `fort.host` (string): hosted Fort endpoint URL (must be HTTPS for production runtime)
- `fort.runtime_host` (string): Fort endpoint URL intended for runtime workers (defaults to `fort.host` when unset)

CLI and runtime behavior:
- `si fort config show` reads these values.
- `si fort config set ...` writes these values to settings.
- `si fort` injects `--host` from `[fort].host` when no explicit native `--host` flag is passed.
- `si fort` prefers the profile Fort session under `CODEX_HOME/fort/` when `CODEX_HOME` is set by a managed Codex profile runtime.
- `si fort` does not accept caller-supplied `FORT_TOKEN_PATH` / `FORT_REFRESH_TOKEN_PATH` as a normal runtime fallback and does not fall back to the active Codex profile outside `si codex spawn` / `si codex shell`.
- `si fort` fails loudly for runtime secret commands when no usable runtime session exists or runtime refresh fails; it does not silently fall back to host/bootstrap admin auth.
- `si fort` uses host/bootstrap admin token files at `~/.si/fort/bootstrap/admin.token` and `~/.si/fort/bootstrap/admin.refresh.token` only for explicit admin/provisioning commands.
- Treat bootstrap/admin auth as recovery-only; day-to-day Fort use should run through profile-scoped runtime token files provisioned by `si codex spawn` or `si codex shell`.
- Codex profile provisioning explicitly requests a `30d` Fort refresh-session TTL even if Fort's general default refresh-session TTL is shorter.
- Runtime worker token state remains file-backed under `~/.si/codex/profiles/<profile>/fort/`; profile refresh tokens must be rotated in place.

### Orbit Fort secret materialization
All `si orbit ...` provider accounts support a shared Fort-backed secret binding shape:

- `fort_repo` (string): default Fort repo for this account's secret bindings.
- `fort_env` (string): default Fort env for this account's secret bindings.
- `fort_prefix` (string): default key prefix for derived Fort keys. If unset, `vault_prefix` remains the compatibility prefix.
- `secrets` (table): logical secret names mapped to either `fort://<repo>/<env>/<key>` or to a key name resolved with `fort_repo` and `fort_env`.

Example:

```toml
[cloudflare.accounts.viva]
fort_repo = "viva"
fort_env = "prod"
fort_prefix = "VIVA_CLOUDFLARE_USER"

[cloudflare.accounts.viva.secrets]
api_token = "VIVA_CLOUDFLARE_USER_API_TOKEN"
```

Orbit commands materialize configured Fort secrets through the current `si fort` runtime session before provider clients are built. Do not pass secrets or `fort://...` references through command flags for normal use, and do not wrap orbit commands in `si fort run`.

When a provider has no Fort binding configured, legacy env-var fallbacks continue to work for compatibility. Literal secret flags are reserved for emergency overrides and should not be used in scripts or shared shell history.

### `[stripe]`
Defaults for `si orbit stripe` account and environment context.
- `stripe.organization` (string): optional organization label
- `stripe.default_account` (string): default account alias (or `acct_` id)
- `stripe.default_env` (string): `live` or `sandbox` (default: `sandbox`)
- `stripe.log_file` (string): JSONL log path for Stripe bridge request/response events (default: `~/.si/logs/stripe.log`)

#### `[stripe.accounts.<alias>]`
Per-account Stripe settings.
- `id` (string): Stripe account id (`acct_...`) used for scoped calls
- `name` (string): display name
- `fort_repo`, `fort_env`, `fort_prefix`, `secrets` (Fort bindings): see "Orbit Fort secret materialization"
- `live_key` (string): direct live API key (prefer env refs instead)
- `sandbox_key` (string): direct sandbox API key (prefer env refs instead)
- `live_key_env` (string): env var name holding the live key
- `sandbox_key_env` (string): env var name holding the sandbox key

Credential resolution order for `si orbit stripe`:
1. Configured Fort binding for the selected account/environment
2. Emergency CLI override (`--api-key`, or sync key flags)
3. Account settings key (`live_key` / `sandbox_key`)
4. Account settings env ref (`live_key_env` / `sandbox_key_env`)
5. Environment-specific env fallback (`SI_STRIPE_LIVE_API_KEY` / `SI_STRIPE_SANDBOX_API_KEY`)
6. Generic env fallback (`SI_STRIPE_API_KEY`)

`SI_STRIPE_ACCOUNT` can provide default account selection when settings do not specify one.

### `[github]`
Defaults for `si orbit github` (GitHub App or OAuth token auth).
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
- `fort_repo`, `fort_env`, `fort_prefix`, `secrets` (Fort bindings): see "Orbit Fort secret materialization"
- `oauth_access_token` (string): direct OAuth token (prefer env refs)
- `oauth_token_env` (string): env var with OAuth token
- `app_id` (int): direct app id (prefer env refs for secretless settings)
- `app_id_env` (string): env var with app id
- `app_private_key_pem` (string): direct private key PEM (prefer env refs)
- `app_private_key_env` (string): env var with private key PEM
- `installation_id` (int): explicit installation id
- `installation_id_env` (string): env var with installation id

Auth mode resolution for `si orbit github`:
1. CLI override (`--auth-mode` where available)
2. Account settings (`auth_mode`)
3. Env fallback (`GITHUB_AUTH_MODE`, then `GITHUB_DEFAULT_AUTH_MODE`)
4. Global settings (`github.default_auth_mode`)

Credential resolution for `si orbit github` in `app` mode:
1. Configured Fort binding for `app_private_key`
2. Emergency CLI overrides (`--app-id`, `--app-key`, `--installation-id`)
3. Account settings (`app_id`, `app_private_key_pem`, `installation_id`)
4. Account env refs (`app_id_env`, `app_private_key_env`, `installation_id_env`)
5. Account-prefix env keys (`GITHUB_<ACCOUNT>_APP_ID`, `GITHUB_<ACCOUNT>_APP_PRIVATE_KEY_PEM`, `GITHUB_<ACCOUNT>_INSTALLATION_ID`)
6. Global env fallbacks (`GITHUB_APP_ID`, `GITHUB_APP_PRIVATE_KEY_PEM`, `GITHUB_INSTALLATION_ID`)

Credential resolution for `si orbit github` in `oauth` mode:
1. Configured Fort binding for `oauth_token`
2. Emergency CLI override (`--token` where available)
3. Account settings (`oauth_access_token`)
4. Account env ref (`oauth_token_env`)
5. Account-prefix env keys (`GITHUB_<ACCOUNT>_OAUTH_ACCESS_TOKEN`, `GITHUB_<ACCOUNT>_TOKEN`)
6. Global env fallbacks (`GITHUB_OAUTH_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`)

### `[cloudflare]`
Defaults for `si orbit cloudflare` (token auth with multi-account and env context labels).
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
- `fort_repo`, `fort_env`, `fort_prefix`, `secrets` (Fort bindings): see "Orbit Fort secret materialization"
- `default_zone_id` (string): default zone id fallback
- `default_zone_name` (string): default zone name fallback
- `prod_zone_id` (string): zone id used when `env=prod`
- `staging_zone_id` (string): zone id used when `env=staging`
- `dev_zone_id` (string): zone id used when `env=dev`
- `api_token_env` (string): env var with API token

Credential resolution for `si orbit cloudflare` is Fort-backed, env-compatible, and token-only:
1. Configured Fort binding for `api_token` or legacy `api_token_fort_*`
2. Emergency CLI overrides (`--api-token`, `--account-id`, `--zone-id`)
3. Account settings (`account_id`, env-mapped zone ids, defaults)
4. Account env refs (`account_id_env`, `api_token_env`)
5. Account-prefix env keys (`CLOUDFLARE_<ACCOUNT>_API_TOKEN`, `CLOUDFLARE_<ACCOUNT>_ACCOUNT_ID`, `CLOUDFLARE_<ACCOUNT>_PROD_ZONE_ID`, `CLOUDFLARE_<ACCOUNT>_STAGING_ZONE_ID`, `CLOUDFLARE_<ACCOUNT>_DEV_ZONE_ID`)
6. Global env fallbacks (`CLOUDFLARE_API_TOKEN`, `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_ZONE_ID`)

### `[gcp]`
Defaults for `si orbit gcp` (Service Usage, IAM, API keys, Gemini, and Vertex AI).
- `gcp.default_account` (string): default account alias
- `gcp.default_env` (string): `prod`, `staging`, or `dev` (default: `prod`)
- `gcp.api_base_url` (string): default API base URL used by `si orbit gcp service` (default: `https://serviceusage.googleapis.com`)
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

Credential resolution for `si orbit gcp` project id:
1. CLI override (`--project`)
2. Account settings (`project_id`)
3. Account env ref (`project_id_env`)
4. Account-prefix env key (`GCP_<ACCOUNT>_PROJECT_ID`)
5. Global env fallbacks (`GCP_PROJECT_ID`, `GOOGLE_CLOUD_PROJECT`)

Credential resolution for `si orbit gcp` OAuth token:
1. CLI override (`--access-token`)
2. Account env ref (`access_token_env`)
3. Account-prefix env key (`GCP_<ACCOUNT>_ACCESS_TOKEN`)
4. Global env fallbacks (`GOOGLE_OAUTH_ACCESS_TOKEN`, `GCP_ACCESS_TOKEN`)

Credential resolution for Gemini API-key mode (`si orbit gcp gemini`):
1. CLI override (`--api-key`)
2. Account env ref (`api_key_env`)
3. Account-prefix env key (`GCP_<ACCOUNT>_API_KEY`)
4. Global env fallbacks (`GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GCP_API_KEY`)

### `[google]`
Defaults for `si orbit google places` and `si orbit google youtube` (multi-account and env context labels).
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

Credential resolution for `si orbit google places` is Fort-backed, env-compatible, and API-key based:
1. CLI overrides (`--api-key`, `--project-id`)
2. Account settings (`project_id`)
3. Account env refs (`project_id_env`, `places_api_key_env`, `prod_places_api_key_env`, `staging_places_api_key_env`, `dev_places_api_key_env`)
4. Account-prefix env keys (`GOOGLE_<ACCOUNT>_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_PROD_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_STAGING_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_DEV_PLACES_API_KEY`, `GOOGLE_<ACCOUNT>_PROJECT_ID`)
5. Global env fallbacks (`GOOGLE_PLACES_API_KEY`, `GOOGLE_PROJECT_ID`)

### `[google.youtube]`
Defaults for `si orbit google youtube` (YouTube Data API v3).
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

Credential resolution for `si orbit google youtube` is Fort-backed, env-compatible, and supports both API key and OAuth:
1. CLI overrides (`--api-key`, `--project-id`, `--client-id`, `--client-secret`, `--redirect-uri`, `--access-token`, `--refresh-token`)
2. Account settings (`project_id`)
3. Account env refs (`project_id_env`, `youtube_api_key_env`, env-specific api key refs, OAuth refs)
4. Account-prefix env keys (`GOOGLE_<ACCOUNT>_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_API_KEY`, `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_ID`, `GOOGLE_<ACCOUNT>_YOUTUBE_CLIENT_SECRET`, `GOOGLE_<ACCOUNT>_YOUTUBE_REDIRECT_URI`, `GOOGLE_<ACCOUNT>_YOUTUBE_ACCESS_TOKEN`, `GOOGLE_<ACCOUNT>_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_PROD_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_STAGING_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_<ACCOUNT>_DEV_YOUTUBE_REFRESH_TOKEN`)
5. Global env fallbacks (`GOOGLE_YOUTUBE_API_KEY`, `GOOGLE_YOUTUBE_CLIENT_ID`, `GOOGLE_YOUTUBE_CLIENT_SECRET`, `GOOGLE_YOUTUBE_REDIRECT_URI`, `GOOGLE_YOUTUBE_ACCESS_TOKEN`, `GOOGLE_YOUTUBE_REFRESH_TOKEN`, `GOOGLE_PROJECT_ID`)

Local OAuth token cache for `si orbit google youtube auth login` is stored at:
- `~/.si/google/youtube/oauth_tokens.json`

### `[vault]`
Defaults for `si vault`.
- `vault.file` (string): default dotenv file used when `--env-file`/`--file` is not provided (default: `.env`)
- `vault.trust_store` (string): optional trust store path for recipient fingerprint checks
- `vault.audit_log` (string): optional local JSONL audit sink (empty by default)
- `vault.key_backend` (string): key backend for SI Vault identity material (`keyring`/`file`)
- `vault.key_file` (string): key file path when `vault.key_backend=\"file\"`
- `vault.sync_backend` (string): Fort-only mode; only `fort` is accepted.

### `[viva]`
Defaults for `si viva` wrapper and Viva tunnel profile config.
- `viva.repo` (string): default local `viva` repo path.
- `viva.bin` (string): default `viva` binary path.
- `viva.build` (bool): default `--build` behavior for wrapper executions.

#### `[viva.tunnel]`
- `viva.tunnel.default_profile` (string): default profile used by `viva tunnel` when `--profile` is omitted.

#### `[viva.tunnel.profiles.<name>]`
Per-profile Cloudflare tunnel runtime settings consumed by `viva tunnel`.
- `name` (string): logical tunnel name.
- `runtime_name` (string): process label used for the Cloudflared runtime.
- `tunnel_id_env_key` (string): dotenv key for Cloudflare tunnel id (default: `VIVA_CLOUDFLARE_TUNNEL_ID`).
- `credentials_env_key` (string): dotenv key for tunnel credentials JSON (default: `CLOUDFLARE_TUNNEL_CREDENTIALS_JSON`).
- `metrics_addr` (string): cloudflared metrics bind address.
- `no_autoupdate` (bool): pass `--no-autoupdate`.
- `runtime_dir` (string): host runtime directory for generated files.
- `fort_env_file` (string): encrypted dotenv file path used by `si fort`; Viva infers the repo/env scope from the canonical `/path/to/<repo>/.env.dev|.env.prod` file path.

##### `[[viva.tunnel.profiles.<name>.routes]]`
- `hostname` (string, optional): ingress hostname.
- `service` (string, required): upstream service URL or `http_status:404`.

### `[shell.prompt]`
Prompt rendering for `si codex shell` interactive shells. This applies without modifying `.bashrc`.
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
workspace_root = "~/code"

[codex]
image = "aureuma/si:local"
network = "si"
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
key_backend = "keyring"
key_file = ""
sync_backend = "fort"

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
