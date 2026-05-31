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
- `paths.workspace_root` (string): optional host directory containing sibling repos. Used by SI runtime commands when flags are omitted.

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
- `SI_NUCLEUS_PROFILE_MAX_WORKERS`: default max worker slots per profile for dispatch (`1` when unset)
- `SI_NUCLEUS_PROFILE_MAX_WORKERS_<PROFILE>`: per-profile max override where `<PROFILE>` is uppercased and `-` is replaced with `_`
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
- Worker-slot behavior is command-level:
  - `si codex spawn|respawn --profile <profile> --slot <slot>`
  - `si codex shell|tail|tmux|stop|remove --profile <profile> --slot <slot>`
  - `si codex repair-auth --profile <profile> --slot <slot>` for in-place Fort runtime repair

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
- Runtime worker token state remains file-backed under:
  - `~/.si/codex/profiles/<profile>/fort/` for the `primary` slot
  - `~/.si/codex/profiles/<profile>/workers/<slot>/fort/` for non-primary slots
  - Fort runtime agent IDs are slot-aware: `si-codex-<profile>` for `primary`, `si-codex-<profile>--<slot>` for non-primary slots
  - profile refresh tokens must be rotated in place.

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

